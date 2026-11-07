package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageCreationVersion        = 4
	storageCreationPrivate        = 1
	storageCreationLocalPrivate   = 4
	storageCreationHistory        = 2
	storageCreationAncestor       = 3
	storageCreationConfigured     = 1
	storageCreationContained      = 2
	storageCreationPersonal       = 3
	storageCreationOwed           = 0
	storageCreationCaptured       = 1
	storageCreationDurable        = 2
	storageCreationMaxComponents  = storagebudget.MaxComponents
	storageCreationMaxGroups      = 3 // History, pointer Control, original host Control
	storageCreationTailBytes      = 4096
	storageCreationMaximumEncoded = 12917 // 128+730+3*819+13*25+2+4098+1081+4096
)

// One closed monotone action, not a regular-file publication. Parent references
// precede child references: [0,Parents) are retained originals; Parents+i refers
// to an earlier captured directory effect. Existing objects are not effects.
// No record contains payload bytes or a reconstructed native capability.
type storageCreationEffect struct {
	Parent, Kind, State byte
	Name                string
	Bytes               uint64
	Payload             storagebudget.Digest
	Native              storageOwnerPersistentObject
}
type storageCreationParent struct {
	Native          storageOwnerPersistentObject
	Role, Component byte
}

type storageCreationIntent struct {
	CapacityPolicy uint8
	Core           storageIntentRecord
	Kind, Branch   byte
	SelectedPath   string
	Parents        []storageCreationParent
	Effects        []storageCreationEffect
	Tail           storageCreationTail
}

// The tail is a pre-effect policy estimate for the next exact enrollment, not a Scope,
// census or synthetic Q0. It stays charged after action Q retirement and is
// discharged only by actual child/control inclusion in native-owned Completed
// evidence. The same original action host page supplies its durable identity.
type storageCreationTail struct {
	Installation            storagebudget.ID
	QBinding                storagebudget.Digest
	Bridge                  storagebudget.ID
	Config, Anchor, Project storageOwnerPersistentObject
	Qualification           storagebudget.Digest
	Targets                 [8]storageOwnerPersistentObject
	Before                  [8]uint64
	Catalogs                [2]storagebudget.Digest
	MaximumExtent           uint64
	ParentPeaks             [2]storagebudget.CapacityVector
	Logical                 storagebudget.Vector
	Capacity                [][80]byte
}

func storageCreationTailDimensions(existing bool, before [8]uint64) ([2]storagebudget.CapacityVector, storagebudget.Vector, error) {
	var out [2]storagebudget.CapacityVector
	if before[4] != 8388608 || before[5] != 8388608 || before[6] != 4194304 || before[7] != 4194304 {
		return out, storagebudget.Vector{}, storagebudget.ErrBounds
	}
	paths := []byte{1}
	if existing {
		paths = []byte{2, 3}
	}
	for _, path := range paths {
		shape := storageEnrollmentPeakScheduleShape{Path: path, Final: [2]uint64{storageEnrollmentPeakEvidenceMax, storageEnrollmentPeakEvidenceMax}}
		var parents [2]storagebudget.CapacityVector
		for _, group := range storageEnrollmentPeakPlanShape(shape) {
			cost, ok := storageEnrollmentPeakEstimate(group)
			if !ok || group.Parent < 1 || group.Parent > 2 {
				return out, storagebudget.Vector{}, storagebudget.ErrBounds
			}
			row := &parents[group.Parent-1]
			if row.Bytes > ^uint64(0)-cost.Bytes || row.Inodes > ^uint64(0)-cost.Inodes {
				return out, storagebudget.Vector{}, storagebudget.ErrBounds
			}
			row.Bytes += cost.Bytes
			row.Inodes += cost.Inodes
		}
		// Axis-wise upper bounds are conservative reservation operands; they are
		// never represented as an executed schedule or a native receipt.
		for i := range out {
			out[i].Bytes = max(out[i].Bytes, parents[i].Bytes)
			out[i].Inodes = max(out[i].Inodes, parents[i].Inodes)
		}
	}
	// Keep the entire action-control completion bound in the same retained
	// escrow: at most7+2*4=15 host pages, full512KiB manifest, three ticket
	// publications (Reserve/Advance/Settle), and one4MiB retirement checkpoint.
	// Manifest writes are full fixed512KiB even for a tiny closed action.
	// These are reservation operands, never certificates of native behavior.
	for _, group := range storageCreationControlPlan() {
		cost, ok := storageEnrollmentPeakEstimate(group)
		if !ok || group.Parent < 1 || group.Parent > 2 {
			return out, storagebudget.Vector{}, storagebudget.ErrBounds
		}
		parent := &out[group.Parent-1]
		if parent.Bytes > ^uint64(0)-cost.Bytes || parent.Inodes > ^uint64(0)-cost.Inodes {
			return out, storagebudget.Vector{}, storagebudget.ErrBounds
		}
		parent.Bytes += cost.Bytes
		parent.Inodes += cost.Inodes
	}
	logical := storagebudget.Vector{Bytes: 2*storageEnrollmentPeakEvidenceMax + 8192, Files: 4}
	if existing {
		if before[0] == 0 || before[1] == 0 || before[0] > storageEnrollmentPeakEvidenceMax || before[1] > storageEnrollmentPeakEvidenceMax || before[0]%65536 != 0 || before[1]%65536 != 0 || before[2] != 4096 || before[3] != 4096 {
			return out, storagebudget.Vector{}, storagebudget.ErrBounds
		}
		logical = storagebudget.Vector{Bytes: 2*storageEnrollmentPeakEvidenceMax - before[0] - before[1]}
	} else {
		for i := 0; i < 4; i++ {
			if before[i] != 0 {
				return out, storagebudget.Vector{}, storagebudget.ErrBounds
			}
		}
	}
	return out, logical, nil
}

func storageCreationControlPlan() []storageEnrollmentPeakGroup {
	out := make([]storageEnrollmentPeakGroup, 0, 20)
	for i := 0; i < 15; i++ {
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(i + 1), Parent: 1, Bytes: storageIntentPage, MetadataOps: 16})
	}
	out = append(out, storageEnrollmentPeakGroup{ID: 16, Parent: 2, Bytes: storagebudget.ManifestBytes, MetadataOps: 28})
	for i := 0; i < 3; i++ {
		out = append(out, storageEnrollmentPeakGroup{ID: uint16(17 + i), Parent: 2, Bytes: storageIntentPage, MetadataOps: 28})
	}
	out = append(out, storageEnrollmentPeakGroup{ID: 20, Parent: 2, Bytes: storagebudget.CheckpointBytes, MetadataOps: 28})
	return out
}

func (r storageCreationTail) validate() error {
	if r.Installation == (storagebudget.ID{}) || r.QBinding == (storagebudget.Digest{}) || r.Bridge == (storagebudget.ID{}) || !r.Config.valid(1) || !r.Anchor.valid(1) || !r.Project.valid(1) || r.Qualification == (storagebudget.Digest{}) || r.MaximumExtent != storageEnrollmentPeakEvidenceMax || len(r.Capacity) < 1 || len(r.Capacity) > 8 {
		return storagebudget.ErrBounds
	}
	existing := r.Before[0] != 0
	peaks, logical, err := storageCreationTailDimensions(existing, r.Before)
	if err != nil || r.ParentPeaks != peaks || r.Logical != logical {
		return storagebudget.ErrBounds
	}
	seen := map[storageOwnerPersistentObject]bool{}
	for i, n := range r.Targets {
		if i < 4 && !existing {
			if n != (storageOwnerPersistentObject{}) {
				return storagebudget.ErrBounds
			}
			continue
		}
		if !n.valid(2) || seen[n] {
			return storagebudget.ErrBounds
		}
		parent := r.Config
		if i >= 6 {
			parent = r.Anchor
		}
		if !storageIdentitySameVolume(parent, n) {
			return storagebudget.ErrBounds
		}
		seen[n] = true
	}
	for _, d := range r.Catalogs {
		if d == (storagebudget.Digest{}) {
			return storagebudget.ErrBounds
		}
	}
	masks := byte(0)
	for i, raw := range r.Capacity {
		mask := raw[24]
		if storageEnrollmentPeakZero(raw[:16]) || storageEnrollmentPeakU64(raw[16:24]) == 0 || mask < 1 || mask > 3 || !storageEnrollmentPeakZero(raw[25:32]) || i > 0 && bytes.Compare(r.Capacity[i-1][:16], raw[:16]) >= 0 {
			return storagebudget.ErrBounds
		}
		expected := storagebudget.CapacityVector{}
		for parent := 0; parent < 2; parent++ {
			fs := raw[48+parent*16 : 64+parent*16]
			if mask&(1<<parent) == 0 {
				if !storageEnrollmentPeakZero(fs) {
					return storagebudget.ErrBounds
				}
				continue
			}
			if storageEnrollmentPeakZero(fs) {
				return storagebudget.ErrBounds
			}
			expected.Bytes += peaks[parent].Bytes
			expected.Inodes += peaks[parent].Inodes
		}
		if storageEnrollmentPeakU64(raw[32:40]) != expected.Bytes || storageEnrollmentPeakU64(raw[40:48]) != expected.Inodes {
			return storagebudget.ErrBounds
		}
		masks |= mask
	}
	if masks != 3 {
		return storagebudget.ErrBounds
	}
	return nil
}

func storageCreationTailCodec(raw []byte, r *storageCreationTail, read bool) bool {
	if len(raw) != storageCreationTailBytes {
		return false
	}
	c := storageIntentCodec{b: raw}
	if read {
		if c.get(2) != 1 {
			c.bad = true
		}
	} else {
		c.put(1, 2)
	}
	c.id(&r.Installation, read)
	c.digest(&r.QBinding, read)
	c.id(&r.Bridge, read)
	storageIdentityIntentTuple(&c, &r.Config, read)
	storageIdentityIntentTuple(&c, &r.Anchor, read)
	storageIdentityIntentTuple(&c, &r.Project, read)
	c.digest(&r.Qualification, read)
	for i := range r.Targets {
		storageIdentityIntentTuple(&c, &r.Targets[i], read)
	}
	for i := range r.Before {
		c.number(&r.Before[i], read)
	}
	for i := range r.Catalogs {
		c.digest(&r.Catalogs[i], read)
	}
	c.number(&r.MaximumExtent, read)
	for i := range r.ParentPeaks {
		c.number(&r.ParentPeaks[i].Bytes, read)
		c.number(&r.ParentPeaks[i].Inodes, read)
	}
	c.vector(&r.Logical, read)
	n := len(r.Capacity)
	if read {
		n = int(c.get(1))
		if n < 1 || n > 8 {
			return false
		}
		r.Capacity = make([][80]byte, n)
	} else {
		c.put(uint64(n), 1)
	}
	for i := 0; i < 8; i++ {
		if i < n {
			if read {
				c.take(r.Capacity[i][:])
			} else {
				c.raw(r.Capacity[i][:])
			}
		} else {
			c.skip(80, read)
		}
	}
	c.skip(len(raw)-c.at, read)
	return !c.bad
}

func (r storageCreationIntent) planDigest() storagebudget.Digest {
	// Fields that evolve with original observations, Q lineage and settlement
	// are excluded. Every effect operand and the retained full tail is included.
	// Full hashing keeps every reconstructed operand, including all16 possible
	// attribution rows. 21020 bounds2+4098+1+2*37+16*722+1+4*299+4096;
	// existing count1..3 hash input bytes are unchanged.
	w := storageEnrollmentPeakWriter{max: 21020}
	w.u8(r.Kind)
	w.u8(r.Branch)
	w.data([]byte(r.SelectedPath), 4096)
	w.u8(byte(len(r.Parents)))
	for _, p := range r.Parents {
		w.put(storageEnrollmentPeakNativeBytes(p.Native))
		w.u8(p.Role)
		w.u8(p.Component)
	}
	for i, component := range r.Core.Components {
		raw := make([]byte, 400)
		c := storageIntentCodec{b: raw}
		c.component(&component, false)
		if c.bad || c.at != 400 || i >= len(r.Core.RootIdentities) || i >= len(r.Core.FilesystemIdentities) || i >= len(r.Core.Locators) {
			return storagebudget.Digest{}
		}
		w.put(raw)
		w.put(r.Core.RootIdentities[i][:])
		w.put(r.Core.FilesystemIdentities[i][:])
		w.data([]byte(r.Core.Locators[i]), 256)
	}
	w.u8(byte(len(r.Effects)))
	for _, e := range r.Effects {
		w.u8(e.Parent)
		w.u8(e.Kind)
		w.data([]byte(e.Name), 255)
		w.u64(e.Bytes)
		w.put(e.Payload[:])
	}
	tail := make([]byte, storageCreationTailBytes)
	if r.Kind == storageCreationLocalPrivate {
		if !reflect.DeepEqual(r.Tail, storageCreationTail{}) {
			return storagebudget.Digest{}
		}
	} else if !storageCreationTailCodec(tail, &r.Tail, false) {
		return storagebudget.Digest{}
	}
	if w.bad {
		return storagebudget.Digest{}
	}
	w.put(tail)
	if w.bad {
		return storagebudget.Digest{}
	}
	domain := "desk-monotone-creation-plan-v1"
	if r.Kind == storageCreationLocalPrivate {
		domain = "desk-local-private-creation-v1"
	}
	if r.CapacityPolicy == storageEnrollmentCapacityEstimatePolicy {
		domain += "-capacity-estimate-v1"
	}
	return storageEnrollmentPeakHash(domain, w.raw)
}

func (r storageCreationIntent) requireCapacityEstimate() error {
	if r.CapacityPolicy != storageEnrollmentCapacityEstimatePolicy {
		return errStorageEnrollmentPeakMigration
	}
	return nil
}

func (r storageCreationIntent) validate() error {
	if r.CapacityPolicy != 0 && r.CapacityPolicy != storageEnrollmentCapacityEstimatePolicy {
		return storagebudget.ErrBounds
	}
	c := r.Core
	local := r.Kind == storageCreationLocalPrivate
	tailValid := r.Tail.validate() == nil
	if local {
		tailValid = reflect.DeepEqual(r.Tail, storageCreationTail{})
	}
	if storageMonotoneIntentCoreValidate(c, true) != nil || len(c.Components) > storageCreationMaxComponents || ((r.Kind == storageCreationHistory || local) && len(r.Effects) > 3) || !tailValid || r.Kind < 1 || r.Kind > 4 || r.Branch < 1 || r.Branch > 3 || !storageOwnerAbsolute([]byte(r.SelectedPath)) || c.Workflow != "desk-monotone-private-creation-v1" || c.Operation != "p:"+hex.EncodeToString(c.CASBinding[:])+":create" || c.CASBinding != r.planDigest() || c.OldExists || c.OldIdentity != (storageDirectoryIdentity{}) || c.OldLength != 0 || c.OldLinkCount != 0 || c.StageExists || c.StageIdentity != (storageDirectoryIdentity{}) || len(r.Parents) < 1 || len(r.Parents) > 2 || len(r.Effects) < 1 || len(r.Effects) > 4 || len(r.Parents)+len(r.Effects) > 5 {
		return storagebudget.ErrUnknown
	}
	if local && (r.Branch != storageCreationContained || len(r.Parents) < 1 || len(r.Parents) > 2 || r.Parents[0].Role != 2 || len(r.Parents) == 2 && r.Parents[1].Role != 3 || c.Target != "dialect-drafts" || !dialectDraftStoragePathSelected(r.SelectedPath)) {
		return storagebudget.ErrUnknown
	}
	groups := 0
	for i := range c.Components {
		if storageCreationAttributionLeader(c, i) < 0 {
			groups++
		}
	}
	maximum := 2
	if r.Kind == storageCreationHistory || local {
		maximum = storageCreationMaxGroups
	}
	if groups > maximum {
		return storagebudget.ErrBounds
	}
	for i, p := range r.Parents {
		if !p.Native.valid(1) || p.Role < 1 || p.Role > 7 || int(p.Component) >= len(c.Components) && !(r.Kind == storageCreationHistory && p.Role == 6 && p.Component == 255) {
			return storagebudget.ErrUnknown
		}
		for j := 0; j < i; j++ {
			if p.Native == r.Parents[j].Native && !(r.Kind == storageCreationHistory && p.Role == 5 && (r.Parents[j].Role == 4 || r.legacyConfigSelection() && r.Parents[j].Role == 6)) {
				return storagebudget.ErrUnknown
			}
		}
	}
	total := uint64(0)
	owed := false
	durable := true
	for i, e := range r.Effects {
		if e.Parent >= byte(len(r.Parents)+i) || !storageOwnerCatalogNameComponent([]byte(e.Name)) || len(e.Name) > 255 || e.Kind < 1 || e.Kind > 2 || e.State > storageCreationDurable {
			return storagebudget.ErrUnknown
		}
		if int(e.Parent) >= len(r.Parents) {
			parent := r.Effects[int(e.Parent)-len(r.Parents)]
			if parent.Kind != 1 || parent.State < storageCreationCaptured && e.State != storageCreationOwed {
				return storagebudget.ErrUnknown
			}
		}
		originalParent := e.Parent
		for int(originalParent) >= len(r.Parents) {
			originalParent = r.Effects[int(originalParent)-len(r.Parents)].Parent
		}
		if int(r.Parents[originalParent].Component) >= len(c.Components) {
			return storagebudget.ErrUnknown
		}
		category := uint8(storagebudget.Control)
		if r.Kind == storageCreationHistory && e.Name != dataLocationName {
			category = storagebudget.History
		}
		if c.Components[r.Parents[originalParent].Component].Category != category {
			return storagebudget.ErrUnknown
		}
		// No later effect may be captured before every preceding effect is durable.
		if owed && e.State != storageCreationOwed {
			return storagebudget.ErrUnknown
		}
		if e.State != storageCreationDurable {
			owed = true
			durable = false
		}
		if e.Kind == 1 {
			if e.Bytes != 0 || e.Payload != (storagebudget.Digest{}) {
				return storagebudget.ErrUnknown
			}
		} else {
			if e.Bytes > 16384 || e.Payload == (storagebudget.Digest{}) {
				return storagebudget.ErrUnknown
			}
			total += e.Bytes
		}
		if e.State == storageCreationOwed {
			if e.Native != (storageOwnerPersistentObject{}) {
				return storagebudget.ErrUnknown
			}
		} else if !e.Native.valid(e.Kind) {
			return storagebudget.ErrUnknown
		}
		if e.State >= storageCreationCaptured {
			parentNative := storageOwnerPersistentObject{}
			if int(e.Parent) < len(r.Parents) {
				parentNative = r.Parents[e.Parent].Native
			} else {
				parentNative = r.Effects[int(e.Parent)-len(r.Parents)].Native
			}
			if !storageIdentitySameVolume(parentNative, e.Native) {
				return storagebudget.ErrUnknown
			}
		}
		for j := 0; j < i; j++ {
			old := r.Effects[j]
			if old.Parent == e.Parent && old.Name == e.Name || e.Native != (storageOwnerPersistentObject{}) && e.Native == old.Native {
				return storagebudget.ErrUnknown
			}
		}
	}
	if total != c.NewLength || r.validateNames() != nil {
		return storagebudget.ErrUnknown
	}
	if c.Phase == storageIntentPrepared || c.Phase == storageIntentReserved || c.Phase == storageIntentRejected {
		for _, e := range r.Effects {
			if e.State != storageCreationOwed {
				return storagebudget.ErrUnknown
			}
		}
	}
	if c.Phase == storageIntentPublishing || c.Phase >= storageIntentSettling && c.Phase <= storageIntentRetired {
		if !durable {
			return storagebudget.ErrUnknown
		}
	}
	if c.Phase >= storageIntentSettling && c.Phase <= storageIntentRetired {
		if c.Outcome != storageIntentPublished {
			return storagebudget.ErrUnknown
		} // no deletion/zero settlement branch
		var expected [storageCreationMaxComponents]storagebudget.Change
		for _, e := range r.Effects {
			// The producer assigns each original parent to exactly one component;
			// descendants retain that parent's original accounting root/category.
			parent := e.Parent
			for int(parent) >= len(r.Parents) {
				parent = r.Effects[int(parent)-len(r.Parents)].Parent
			}
			component := int(r.Parents[parent].Component)
			if component >= len(c.Components) {
				return storagebudget.ErrUnknown
			}
			v := storagebudget.Vector{}
			if e.Kind == 1 {
				v.Directories = 1
			} else {
				v.Bytes = e.Bytes
				v.Files = 1
			}
			var err error
			expected[component].PhysicalAdd, err = storageVectorAdd(expected[component].PhysicalAdd, v)
			if err != nil {
				return err
			}
			if c.Components[component].Workspace != (storagebudget.ID{}) {
				expected[component].AttributionAdd = expected[component].PhysicalAdd
			}
		}
		for i := range c.Components {
			if leader := storageCreationAttributionLeader(c, i); leader >= 0 {
				expected[i].AttributionAdd = expected[leader].PhysicalAdd
			}
		}
		for i, ch := range c.Changes {
			if ch != expected[i] {
				return storagebudget.ErrUnknown
			}
		}
		v := r
		v.Core.Evidence = storagebudget.Digest{}
		v.Core.Token = storagebudget.Token{}
		v.Core.PreviousToken = storagebudget.Token{}
		v.Core.PreviousPageDigest = storagebudget.Digest{}
		v.Core.Generation = 0
		v.Core.Phase = 0
		v.Core.RetirementEpoch = 0
		raw, err := storageOwnerDigest("desk-monotone-creation-factual-settlement-v1", v)
		if err != nil || raw != c.Evidence {
			return storagebudget.ErrUnknown
		}
	}
	return nil
}

// The legacy case is pointer-only to the original config native. Workflow3
// in the source board independently distinguishes this retained old selection.
func (r storageCreationIntent) legacyConfigSelection() bool {
	return r.Kind == storageCreationHistory && r.Branch != storageCreationContained && len(r.Parents) == 2 && r.Parents[0].Role == 6 && r.Parents[0].Component == 255 && r.Parents[0].Native == r.Tail.Config && r.Parents[1].Role == 5 && r.Parents[1].Native == r.Tail.Config && len(r.Effects) == 1 && r.Effects[0].Parent == 1 && r.Effects[0].Kind == 2 && r.Effects[0].Name == dataLocationName
}

func (r storageCreationIntent) validateNames() error {
	roles := make([]byte, 0, len(r.Parents)+len(r.Effects))
	for _, p := range r.Parents {
		roles = append(roles, p.Role)
	}
	names := map[string]bool{}
	for _, e := range r.Effects {
		if names[e.Name] {
			return storagebudget.ErrUnknown
		}
		names[e.Name] = true
		if int(e.Parent) >= len(roles) {
			return storagebudget.ErrUnknown
		}
		parent := roles[e.Parent]
		child := byte(0)
		switch r.Kind {
		case storageCreationPrivate, storageCreationLocalPrivate:
			if r.Branch != storageCreationContained || (len(r.Parents) != 1 && len(r.Parents) != 2) || r.Parents[0].Role < 1 || r.Parents[0].Role > 3 || len(r.Parents) == 2 && (r.Parents[0].Role != 2 || r.Parents[1].Role != 3) || r.Kind == storageCreationPrivate && r.Core.Target != ".desk-private" || r.Kind == storageCreationLocalPrivate && (r.Core.Target != "dialect-drafts" || r.Parents[0].Role != 2) {
				return storagebudget.ErrUnknown
			}
			switch {
			case r.Kind == storageCreationPrivate && parent == 1 && e.Kind == 1 && e.Name == ".desk-private":
				child = 2
			case parent == 2 && e.Kind == 1 && e.Name == "dialect-drafts":
				child = 3
			case parent == 3 && e.Kind == 2 && e.Name == ".gitignore" && e.Bytes == 2 && e.Payload == sha256.Sum256([]byte("*\n")):
			case parent == 3 && e.Kind == 2 && e.Name == "workflow.lock" && e.Bytes == 0 && e.Payload == sha256.Sum256(nil):
			default:
				return storagebudget.ErrUnknown
			}
		case storageCreationHistory:
			if r.Core.Target != "data" && r.Core.Target != "history" || r.Branch == storageCreationPersonal && r.Core.Target != "history" || r.Branch != storageCreationPersonal && r.Core.Target != "data" {
				return storagebudget.ErrUnknown
			}
			for _, p := range r.Parents {
				if p.Role < 4 || p.Role > 6 {
					return storagebudget.ErrUnknown
				}
			}
			for _, p := range r.Parents {
				if p.Role == 6 && p.Native == r.Tail.Config && !r.legacyConfigSelection() {
					return storagebudget.ErrUnknown
				}
			}
			switch {
			case parent == 4 && e.Kind == 1 && e.Name == r.Core.Target:
				child = 6
			case parent == 6 && e.Kind == 2 && e.Name == dataMarkerName && e.Bytes <= 4096:
			case parent == 5 && e.Kind == 2 && e.Name == dataLocationName && e.Bytes <= 16384:
			default:
				return storagebudget.ErrUnknown
			}
		case storageCreationAncestor:
			if r.Branch != storageCreationPersonal || len(r.Parents) != 1 || r.Parents[0].Role != 7 || len(r.Effects) != 1 || e.Kind != 1 || e.Name != r.Core.Target {
				return storagebudget.ErrUnknown
			}
			child = 7
		default:
			return storagebudget.ErrUnknown
		}
		roles = append(roles, child)
	}
	return nil
}

// Counts1..3 retain the original full-row encoding exactly. Counts4..16 are
// explicit compact mode: tag0 is a full physical group and tag1..16 names an
// earlier physical leader followed by workspace16/generation8. Old readers
// refuse the previously invalid count. Chains and forward aliases are invalid.
// Full reconstructed DTOs use the existing16-component manifest/Q machinery.
func storageCreationAttributionValue(r storageIntentRecord, leader int, workspace storagebudget.ID, generation uint64) (storagebudget.Component, storagebudget.Change) {
	v := r.Components[leader]
	v.Workspace = workspace
	v.WorkspaceGeneration = generation
	v.Attribution = v.Physical
	v.Physical = storagebudget.Vector{}
	v.SettlementPhysical = storagebudget.Vector{}
	v.SettlementAttribution = storagebudget.Vector{}
	for i := range v.Capacity {
		v.Capacity[i].Peak = storagebudget.CapacityVector{}
	}
	return v, storagebudget.Change{AttributionAdd: r.Changes[leader].PhysicalAdd}
}
func storageCreationAttributionLeader(r storageIntentRecord, index int) int {
	if index < 0 || index >= len(r.Components) || len(r.RootIdentities) != len(r.Components) || len(r.FilesystemIdentities) != len(r.Components) || len(r.Locators) != len(r.Components) {
		return -1
	}
	row := r.Components[index]
	if row.Workspace == (storagebudget.ID{}) {
		return -1
	}
	for i := 0; i < index; i++ {
		base := r.Components[i]
		if base.Physical == (storagebudget.Vector{}) || base.Root != row.Root || base.Category != row.Category || base.Workspace == row.Workspace {
			continue
		}
		v := base
		v.Workspace = row.Workspace
		v.WorkspaceGeneration = row.WorkspaceGeneration
		v.Attribution = v.Physical
		v.Physical = storagebudget.Vector{}
		v.SettlementPhysical = storagebudget.Vector{}
		v.SettlementAttribution = storagebudget.Vector{}
		for j := range v.Capacity {
			v.Capacity[j].Peak = storagebudget.CapacityVector{}
		}
		if v == row && r.RootIdentities[i] == r.RootIdentities[index] && r.FilesystemIdentities[i] == r.FilesystemIdentities[index] && r.Locators[i] == r.Locators[index] {
			return i
		}
	}
	return -1
}
func storageCreationCoreCodec(c *storageIntentCodec, r *storageIntentRecord, read bool) bool {
	c.id(&r.OperationID, read)
	c.digest(&r.PreviousPageDigest, read)
	c.digest(&r.CASBinding, read)
	c.number(&r.AdmissionEpoch, read)
	c.number(&r.PolicyRevision, read)
	c.number(&r.RetirementEpoch, read)
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
		if n < 1 || n > storageCreationMaxComponents {
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
	full := make([]bool, n)
	groups := 0
	for i := 0; i < n; i++ {
		tag := 0
		if n > 3 {
			if read {
				tag = int(c.get(1))
			} else {
				if leader := storageCreationAttributionLeader(*r, i); leader >= 0 {
					tag = leader + 1
				}
				c.put(uint64(tag), 1)
			}
		}
		if tag != 0 {
			leader := tag - 1
			if leader >= i || !full[leader] || r.Components[leader].Physical == (storagebudget.Vector{}) {
				return false
			}
			workspace := r.Components[i].Workspace
			generation := r.Components[i].WorkspaceGeneration
			c.id(&workspace, read)
			c.number(&generation, read)
			if workspace == (storagebudget.ID{}) || workspace == r.Components[leader].Workspace || generation == 0 {
				return false
			}
			row, change := storageCreationAttributionValue(*r, leader, workspace, generation)
			if read {
				r.Components[i] = row
				r.Changes[i] = change
				r.RootIdentities[i] = r.RootIdentities[leader]
				r.FilesystemIdentities[i] = r.FilesystemIdentities[leader]
				r.Locators[i] = r.Locators[leader]
			}
			if storageCreationAttributionLeader(*r, i) != leader || r.Changes[i] != change {
				return false
			}
			continue
		}
		full[i] = true
		groups++
		if n > 3 && groups > storageCreationMaxGroups {
			return false
		}
		c.component(&r.Components[i], read)
		c.digest(&r.RootIdentities[i], read)
		c.digest(&r.FilesystemIdentities[i], read)
		c.change(&r.Changes[i], read)
		c.text(&r.Locators[i], 256, read)
		if n > 3 && storageCreationAttributionLeader(*r, i) >= 0 {
			return false
		}
	}
	c.text(&r.Target, 255, read)
	if read {
		r.Stage = r.Target
	} else if r.Stage != r.Target {
		return false
	}
	c.text(&r.Workflow, 128, read)
	c.text(&r.Operation, 128, read)
	return !c.bad
}

func storageCreationIntentBodyCodec(raw []byte, r *storageCreationIntent, read bool) (int, bool) {
	c := storageIntentCodec{b: raw, at: 128}
	if !storageCreationCoreCodec(&c, &r.Core, read) {
		return 0, false
	}
	if read {
		r.Kind = byte(c.get(1))
		r.Branch = byte(c.get(1))
	} else {
		c.put(uint64(r.Kind), 1)
		c.put(uint64(r.Branch), 1)
	}
	c.text(&r.SelectedPath, 4096, read)
	n := len(r.Parents)
	if read {
		n = int(c.get(1))
		if n < 1 || n > 2 {
			return 0, false
		}
		r.Parents = make([]storageCreationParent, n)
	} else {
		c.put(uint64(n), 1)
	}
	for i := range r.Parents {
		p := &r.Parents[i]
		storageIdentityIntentTuple(&c, &p.Native, read)
		if read {
			p.Role = byte(c.get(1))
			p.Component = byte(c.get(1))
		} else {
			c.put(uint64(p.Role), 1)
			c.put(uint64(p.Component), 1)
		}
	}
	n = len(r.Effects)
	if read {
		n = int(c.get(1))
		if n < 1 || n > 4 || n+len(r.Parents) > 5 {
			return 0, false
		}
		r.Effects = make([]storageCreationEffect, n)
	} else {
		c.put(uint64(n), 1)
	}
	for i := range r.Effects {
		e := &r.Effects[i]
		if read {
			e.Parent = byte(c.get(1))
			e.Kind = byte(c.get(1))
			e.State = byte(c.get(1))
		} else {
			c.put(uint64(e.Parent), 1)
			c.put(uint64(e.Kind), 1)
			c.put(uint64(e.State), 1)
		}
		c.text(&e.Name, 255, read)
		c.number(&e.Bytes, read)
		c.digest(&e.Payload, read)
		storageIdentityIntentTuple(&c, &e.Native, read)
	}
	if c.at+storageCreationTailBytes > len(raw) {
		return 0, false
	}
	if r.Kind == storageCreationLocalPrivate {
		if read {
			r.Tail = storageCreationTail{}
		}
		if !reflect.DeepEqual(r.Tail, storageCreationTail{}) || !storageAllZero(raw[c.at:c.at+storageCreationTailBytes]) {
			return 0, false
		}
	} else if !storageCreationTailCodec(raw[c.at:c.at+storageCreationTailBytes], &r.Tail, read) {
		return 0, false
	}
	c.at += storageCreationTailBytes
	return c.at, !c.bad
}

func storageEncodeCreationIntent(r storageCreationIntent, installation storagebudget.ID, binding storagebudget.Digest) ([]byte, error) {
	if r.validate() != nil || installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) || r.Kind != storageCreationLocalPrivate && (installation != r.Tail.Installation || binding != r.Tail.QBinding) {
		return nil, storagebudget.ErrUnknown
	}
	raw := make([]byte, storageIntentPage)
	copy(raw[:8], "DSKHINT4")
	binary.LittleEndian.PutUint16(raw[8:10], storageCreationVersion)
	if r.Kind == storageCreationLocalPrivate {
		copy(raw[:8], "DSKHINT5")
		binary.LittleEndian.PutUint16(raw[8:10], 5)
		raw[13] = 2
	}
	if r.CapacityPolicy == storageEnrollmentCapacityEstimatePolicy {
		copy(raw[:8], "DSKHINT6")
		binary.LittleEndian.PutUint16(raw[8:10], 6)
		raw[13] = storageEnrollmentCapacityEstimatePolicy
		if r.Kind == storageCreationLocalPrivate {
			copy(raw[:8], "DSKHINT7")
			binary.LittleEndian.PutUint16(raw[8:10], 7)
		}
	}
	binary.LittleEndian.PutUint16(raw[10:12], r.Core.Slot)
	raw[12] = byte(r.Core.Phase)
	binary.LittleEndian.PutUint64(raw[16:24], r.Core.Generation)
	copy(raw[24:40], installation[:])
	copy(raw[40:72], binding[:])
	end, ok := storageCreationIntentBodyCodec(raw, &r, false)
	if !ok || end > storageCreationMaximumEncoded {
		return nil, storagebudget.ErrBounds
	}
	binary.LittleEndian.PutUint32(raw[72:76], uint32(end-128))
	sum := sha256.Sum256(raw)
	copy(raw[76:108], sum[:])
	return raw, nil
}
func storageDecodeCreationIntent(raw []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageCreationIntent, error) {
	var r storageCreationIntent
	if len(raw) != storageIntentPage || slot < 0 || slot >= storagebudget.Slots || installation == (storagebudget.ID{}) || binding == (storagebudget.Digest{}) || !(string(raw[:8]) == "DSKHINT4" && binary.LittleEndian.Uint16(raw[8:10]) == storageCreationVersion && storageAllZero(raw[13:16]) || string(raw[:8]) == "DSKHINT5" && binary.LittleEndian.Uint16(raw[8:10]) == 5 && raw[13] == 2 && storageAllZero(raw[14:16]) || string(raw[:8]) == "DSKHINT6" && binary.LittleEndian.Uint16(raw[8:10]) == 6 && raw[13] == storageEnrollmentCapacityEstimatePolicy && storageAllZero(raw[14:16]) || string(raw[:8]) == "DSKHINT7" && binary.LittleEndian.Uint16(raw[8:10]) == 7 && raw[13] == storageEnrollmentCapacityEstimatePolicy && storageAllZero(raw[14:16])) || int(binary.LittleEndian.Uint16(raw[10:12])) != slot || !bytes.Equal(raw[24:40], installation[:]) || !bytes.Equal(raw[40:72], binding[:]) || !storageAllZero(raw[108:128]) {
		return r, storagebudget.ErrUnknown
	}
	checked := append([]byte{}, raw...)
	clear(checked[76:108])
	sum := sha256.Sum256(checked)
	if !bytes.Equal(raw[76:108], sum[:]) {
		return r, storagebudget.ErrUnknown
	}
	if string(raw[:8]) == "DSKHINT6" || string(raw[:8]) == "DSKHINT7" {
		r.CapacityPolicy = storageEnrollmentCapacityEstimatePolicy
	}
	r.Core.Slot = uint16(slot)
	r.Core.Phase = storageIntentPhase(raw[12])
	r.Core.Generation = binary.LittleEndian.Uint64(raw[16:24])
	end, ok := storageCreationIntentBodyCodec(raw, &r, true)
	if !ok || end > storageCreationMaximumEncoded || uint32(end-128) != binary.LittleEndian.Uint32(raw[72:76]) || !storageAllZero(raw[end:]) || r.Kind != storageCreationLocalPrivate && (r.Tail.Installation != installation || r.Tail.QBinding != binding) || r.validate() != nil || (string(raw[:8]) == "DSKHINT5" || string(raw[:8]) == "DSKHINT7") != (r.Kind == storageCreationLocalPrivate) {
		return storageCreationIntent{}, storagebudget.ErrUnknown
	}
	return r, nil
}

func storageCreationIntentTransition(old, next storageCreationIntent, oldPage []byte) bool {
	if old.validate() != nil || next.validate() != nil || len(oldPage) != storageIntentPage || old.Core.Generation == ^uint64(0) || next.Core.Generation != old.Core.Generation+1 || next.Core.PreviousPageDigest != sha256.Sum256(oldPage) || old.Core.Slot != next.Core.Slot {
		return false
	}
	var installation storagebudget.ID
	var binding storagebudget.Digest
	copy(installation[:], oldPage[24:40])
	copy(binding[:], oldPage[40:72])
	decoded, err := storageDecodeCreationIntent(oldPage, int(old.Core.Slot), installation, binding)
	if err != nil || !reflect.DeepEqual(decoded, old) {
		return false
	}
	if old.Core.reusable() {
		return next.Core.Phase == storageIntentPrepared && next.Core.OperationID != old.Core.OperationID
	}
	if old.Core.OperationID != next.Core.OperationID {
		return false
	}
	a, b := old, next
	a.Core = storageIntentRecord{}
	b.Core = storageIntentRecord{}
	a.Effects = append([]storageCreationEffect{}, a.Effects...)
	b.Effects = append([]storageCreationEffect{}, b.Effects...)
	for i := range a.Effects {
		a.Effects[i].State = 0
		a.Effects[i].Native = storageOwnerPersistentObject{}
	}
	for i := range b.Effects {
		b.Effects[i].State = 0
		b.Effects[i].Native = storageOwnerPersistentObject{}
	}
	if !reflect.DeepEqual(a, b) || old.Core.CASBinding != next.Core.CASBinding || old.Core.AdmissionEpoch != next.Core.AdmissionEpoch || !reflect.DeepEqual(old.Core.Components, next.Core.Components) || !reflect.DeepEqual(old.Core.RootIdentities, next.Core.RootIdentities) || !reflect.DeepEqual(old.Core.FilesystemIdentities, next.Core.FilesystemIdentities) || !reflect.DeepEqual(old.Core.Locators, next.Core.Locators) {
		return false
	}
	changed := 0
	for i, e := range old.Effects {
		n := next.Effects[i]
		if e.State != n.State {
			if n.State != e.State+1 || e.State >= storageCreationDurable {
				return false
			}
			changed++
		} else if e.Native != n.Native {
			return false
		}
		if e.State >= storageCreationCaptured && e.Native != n.Native {
			return false
		}
	}
	if changed > 1 {
		return false
	}
	sameTokens := old.Core.Token == next.Core.Token && old.Core.PreviousToken == next.Core.PreviousToken
	switch old.Core.Phase {
	case storageIntentPrepared:
		return changed == 0 && (next.Core.Phase == storageIntentReserved || next.Core.Phase == storageIntentRejected)
	case storageIntentReserved:
		return changed == 0 && next.Core.Phase == storageIntentStaging && next.Core.PreviousToken == old.Core.Token && storageIntentNextToken(old.Core.Token, next.Core.Token) && old.Core.PolicyRevision == next.Core.PolicyRevision
	case storageIntentStaging:
		return sameTokens && old.Core.PolicyRevision == next.Core.PolicyRevision && (changed == 1 && next.Core.Phase == storageIntentStaging || changed == 0 && next.Core.Phase == storageIntentPublishing)
	case storageIntentPublishing:
		return changed == 0 && sameTokens && old.Core.PolicyRevision == next.Core.PolicyRevision && next.Core.Phase == storageIntentSettling
	case storageIntentSettling:
		return changed == 0 && next.Core.PreviousToken == old.Core.Token && storageIntentNextToken(old.Core.Token, next.Core.Token) && old.Core.Evidence == next.Core.Evidence && reflect.DeepEqual(old.Core.Changes, next.Core.Changes) && old.Core.PolicyRevision == next.Core.PolicyRevision && next.Core.Phase == storageIntentSettled
	case storageIntentSettled:
		return changed == 0 && sameTokens && old.Core.Evidence == next.Core.Evidence && reflect.DeepEqual(old.Core.Changes, next.Core.Changes) && old.Core.PolicyRevision == next.Core.PolicyRevision && next.Core.Phase == storageIntentRetired
	default:
		return false
	}
}

// Host selection explicitly joins J4. Legacy selectors remain byte-for-byte
// refusing readers of the new format. This parser proves lineage only; current
// Q retirement/rejection, immutable mappings and tail discharge remain required
// at the original owner boundary before slot reuse or Reserve.
type storageCreationHostSelection struct {
	Slot                storageIntentSlot
	Creation            *storageCreationIntent
	Identity            *storageIdentityIntent
	PredecessorCreation *storageCreationIntent
	PredecessorIdentity *storageIdentityIntent
}

func storageDecodeCreationHostPage(raw []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageCreationHostSelection, error) {
	var out storageCreationHostSelection
	if len(raw) != storageIntentPage {
		return out, storagebudget.ErrUnknown
	}
	out.Slot.page = append([]byte{}, raw...)
	if storageAllZero(raw) {
		out.Slot.record.Slot = uint16(slot)
		return out, nil
	}
	switch string(raw[:8]) {
	case "DSKHINT2", "DSKHINT8", "DSKHINT9":
		r, err := storageDecodeIntent(raw, slot, installation, binding)
		out.Slot.record = r
		return out, err
	case "DSKHINT3":
		r, err := storageDecodeIdentityIntent(raw, slot, installation, binding)
		out.Slot.record = r.Core
		out.Identity = &r
		return out, err
	case "DSKHINT5":
		if raw[13] == 1 || raw[13] == 3 {
			r, err := storageDecodeIdentityIntent(raw, slot, installation, binding)
			out.Slot.record, out.Identity = r.Core, &r
			return out, err
		}
		if raw[13] != 2 {
			return out, storagebudget.ErrUnknown
		}
		fallthrough
	case "DSKHINT4", "DSKHINT6", "DSKHINT7":
		r, err := storageDecodeCreationIntent(raw, slot, installation, binding)
		out.Slot.record = r.Core
		out.Creation = &r
		return out, err
	case "DSKHINT1":
		return out, storagebudget.ErrUnsupported
	default:
		return out, storagebudget.ErrUnknown
	}
}
func storageSelectCreationHost(a, b []byte, slot int, installation storagebudget.ID, binding storagebudget.Digest) (storageCreationHostSelection, error) {
	var out storageCreationHostSelection
	if len(a) != storageIntentPage || len(b) != storageIntentPage || slot < 0 || slot >= storagebudget.Slots {
		return out, storagebudget.ErrUnknown
	}
	has4 := storageCreationEstimatePage(a) || storageCreationEstimatePage(b) || string(a[:8]) == "DSKHINT4" || string(b[:8]) == "DSKHINT4" || string(a[:8]) == "DSKHINT5" || string(b[:8]) == "DSKHINT5" || !storageAllZero(a) && !storageAllZero(b) && string(a[:8]) != string(b[:8])
	if !has4 {
		old, err := storageSelectIdentityIntent(a, b, slot, installation, binding)
		if err != nil {
			return out, err
		}
		switch old.Kind {
		case storageIdentityIntentBanksEmpty:
			out.Slot = storageIntentSlot{record: storageIntentRecord{Slot: uint16(slot)}, page: make([]byte, storageIntentPage), bank: 1}
		case storageIdentityIntentBanksV2:
			out.Slot = old.Legacy
		case storageIdentityIntentBanksV3:
			out.Slot = storageIntentSlot{record: old.Record.Core, page: old.Page, bank: old.Bank}
			out.Identity = &old.Record
		default:
			return out, storagebudget.ErrUnknown
		}
		return out, nil
	}
	ar, err := storageDecodeCreationHostPage(a, slot, installation, binding)
	if err != nil {
		return out, err
	}
	br, err := storageDecodeCreationHostPage(b, slot, installation, binding)
	if err != nil {
		return out, err
	}
	ar.Slot.bank = 0
	br.Slot.bank = 1
	if ar.Slot.record.Phase == storageIntentEmpty || br.Slot.record.Phase == storageIntentEmpty {
		selected := ar
		if ar.Slot.record.Phase == storageIntentEmpty {
			selected = br
		}
		core := selected.Slot.record
		if selected.Creation == nil && selected.Identity == nil || core.Generation != 1 || core.Phase != storageIntentPrepared || core.PreviousPageDigest != (storagebudget.Digest{}) {
			return out, storagebudget.ErrUnknown
		}
		return selected, nil
	}
	if ar.Slot.record.Generation == br.Slot.record.Generation {
		if !bytes.Equal(a, b) {
			return out, storagebudget.ErrUnknown
		}
		return ar, nil
	}
	old, next := ar, br
	if old.Slot.record.Generation > next.Slot.record.Generation {
		old, next = next, old
	}
	oc, nc := old.Slot.record, next.Slot.record
	if oc.Generation == ^uint64(0) || nc.Generation != oc.Generation+1 || nc.PreviousPageDigest != sha256.Sum256(old.Slot.page) {
		return out, storagebudget.ErrUnknown
	}
	if oc.reusable() && nc.Phase == storageIntentPrepared && nc.OperationID != oc.OperationID {
		// A J4 predecessor's retained tail is checked by the original owner before
		// any reuse. Merely selecting this linked Prepared grants no Q/native call.
		next.PredecessorCreation, next.PredecessorIdentity = old.Creation, old.Identity
		return next, nil
	}
	if old.Identity != nil || next.Identity != nil {
		if old.Identity == nil || next.Identity == nil || !storageIdentityIntentTransition(*old.Identity, *next.Identity, old.Slot.page) {
			return out, storagebudget.ErrUnknown
		}
		return next, nil
	}
	if old.Creation == nil || next.Creation == nil || !storageCreationIntentTransition(*old.Creation, *next.Creation, old.Slot.page) {
		return out, storagebudget.ErrUnknown
	}
	return next, nil
}

// The pure structural half of tail discharge. Actual owners additionally join
// native fixed-file custody, actual selected C/C, Q/census and source currentness.
// A hash or reusable host phase alone never discharges the tail.
func storageCreationTailIncluded(r storageCreationIntent, e storageEnrollmentPeakEvidence) bool {
	return e.Phase == 2 && storageCreationTailBound(r, e)
}

func storageCreationTailBound(r storageCreationIntent, e storageEnrollmentPeakEvidence) bool {
	if (r.CapacityPolicy == storageEnrollmentCapacityEstimatePolicy) != e.Scope.capacityEstimate() {
		return false
	}
	if r.validate() != nil || r.Core.Phase != storageIntentRetired || r.Tail.Installation != e.Installation || r.Tail.QBinding != e.QBinding || e.Phase > 2 || !e.Scope.hasOriginalPeer() || e.Scope.target(9) != r.Tail.Config || e.Scope.target(10) != r.Tail.Anchor || e.Bridge == r.Tail.Bridge && e.Project != r.Tail.Project || e.Scope.final(0) > r.Tail.MaximumExtent || e.Scope.final(1) > r.Tail.MaximumExtent {
		return false
	}
	raw, err := storageEncodeCreationIntent(r, r.Tail.Installation, r.Tail.QBinding)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	covered := false
	for _, w := range e.Witnesses {
		if !w.Selected || w.Digest != digest || w.Creation == nil || int(w.Origin) >= len(e.Board.Origins) {
			continue
		}
		origin := e.Board.Origins[w.Origin]
		if origin.Bridge != r.Tail.Bridge || origin.Epoch != r.Core.RetirementEpoch {
			continue
		}
		for _, coverage := range e.Board.Coverage {
			if coverage.Slot == r.Core.Slot && coverage.Page == digest && coverage.Operation == r.Core.OperationID && coverage.Generation == r.Core.Generation && coverage.Epoch == e.AfterEpoch && coverage.Bridge == e.Bridge {
				covered = true
			}
		}
	}
	if !covered {
		return false
	}
	return storageCreationTailCatalog(r, e.Inner.Catalog)
}

func storageCreationTailCatalog(r storageCreationIntent, catalog storageOwnerCatalogV2) bool {
	if r.validate() != nil || r.Core.Phase != storageIntentRetired {
		return false
	}
	nativeForRole := func(role byte) storageOwnerPersistentObject {
		for _, p := range r.Parents {
			if p.Role == role {
				return p.Native
			}
		}
		for _, effect := range r.Effects {
			if effect.Kind != 1 {
				continue
			}
			if role == 2 && effect.Name == ".desk-private" || role == 6 && (effect.Name == "data" || effect.Name == "history") || role == 7 && r.Kind == storageCreationAncestor {
				return effect.Native
			}
		}
		return storageOwnerPersistentObject{}
	}
	wanted, role := storageOwnerPersistentObject{}, byte(0)
	switch r.Kind {
	case storageCreationPrivate:
		wanted = nativeForRole(2)
		role = 3
	case storageCreationHistory:
		wanted = nativeForRole(6)
		role = 4
		if r.legacyConfigSelection() {
			role = 1
		}
	case storageCreationAncestor:
		wanted = r.Effects[0].Native
		role = 8
	default:
		return false
	}
	if !wanted.valid(1) {
		return false
	}
	found := false
	for _, root := range catalog.Roots {
		if int(root.DirectoryObject) >= len(catalog.Objects) || catalog.Objects[root.DirectoryObject].Persistent != wanted {
			continue
		}
		if root.Role != role && !(role == 3 && root.Role == 6) {
			continue
		}
		if role == 1 && (root.Source.Kind != 1 || !bytes.Equal(root.Source.Path, catalog.OriginalOwner.ConfigPath)) || role == 3 && root.Source.Kind != 8 || role == 4 && root.Source.Kind != 4 || role == 8 && root.Source.Kind != 9 {
			return false
		}
		found = true
	}
	if !found {
		return false
	}
	if r.Kind == storageCreationHistory {
		settings := nativeForRole(5)
		if !settings.valid(1) {
			return false
		}
		selected := false
		for _, control := range catalog.OwnerControls {
			if control.Role == 2 && int(control.ParentObject) < len(catalog.Objects) && catalog.Objects[control.ParentObject].Persistent == settings && string(control.Name) == dataLocationName && control.Location.Version == 1 && string(control.Location.Path) == r.SelectedPath && len(control.Location.Previous) == 0 && control.Location.Selected == wanted {
				selected = true
			}
		}
		if !selected {
			return false
		}
		for _, effect := range r.Effects {
			if effect.Name != dataMarkerName {
				continue
			}
			marker := false
			for _, object := range catalog.Objects {
				if object.Role == 23 && object.Persistent == effect.Native && int(object.Parent) < len(catalog.Objects) && catalog.Objects[object.Parent].Persistent == wanted && string(object.Name) == dataMarkerName {
					marker = true
				}
			}
			if !marker {
				return false
			}
		}
	}
	return true
}

// This is the existing original directory lock lifetime with an explicit
// borrowing edge. It carries no storage mutation/Q/native qualification role.
// One synchronous creator borrows at a time; no registry or ownership flag can
// substitute for this exact original descriptor and its sealed live member.
type storageCreatorLease struct {
	move                          *storageMoveOperation // exact invocation-local borrower; never persisted or accepted as a capability alone
	self                          *storageCreatorLease
	root                          *os.Root
	file                          *os.File
	identity                      os.FileInfo
	mu                            sync.Mutex
	borrowed                      *storageCreatorBorrow
	closing, closed, closeRunning bool
	err                           error
}
type storageCreatorBorrow struct {
	self   *storageCreatorBorrow
	lease  *storageCreatorLease
	parent *os.Root
	closed bool
}

func storageCreatorLockedLease(root *os.Root, file *os.File) (*storageCreatorLease, error) {
	if root == nil || file == nil {
		return nil, storagebudget.ErrAuthority
	}
	info, err := file.Stat()
	named, ne := root.Stat(".")
	if err != nil || ne != nil || !info.IsDir() || !os.SameFile(info, named) {
		return nil, errors.Join(errStorageCustody, err, ne)
	}
	l := &storageCreatorLease{root: root, file: file, identity: info}
	l.self = l
	return l, nil
}
func storageCreatorAcquire(root *os.Root) (*storageCreatorLease, error) {
	if root == nil {
		return nil, storagebudget.ErrAuthority
	}
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if err = lockPrivateData(f, true); err != nil {
		return nil, errors.Join(storagebudget.ErrBusy, err, f.Close())
	}
	l, err := storageCreatorLockedLease(root, f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return l, nil
}
func (l *storageCreatorLease) borrow(ctx context.Context, parent *os.Root) (*storageCreatorBorrow, error) {
	if l == nil || l.self != l || ctx == nil || parent == nil {
		return nil, storagebudget.ErrAuthority
	}
	l.mu.Lock()
	if l.closing || l.closed || l.borrowed != nil || l.err != nil {
		prior := l.err
		l.mu.Unlock()
		return nil, errors.Join(storagebudget.ErrBusy, prior)
	}
	b := &storageCreatorBorrow{lease: l, parent: parent}
	b.self = b
	l.borrowed = b
	l.mu.Unlock()
	if err := b.current(ctx); err != nil {
		return nil, errors.Join(err, b.Close())
	}
	return b, nil
}
func (b *storageCreatorBorrow) current(ctx context.Context) error {
	if b == nil || b.self != b || b.lease == nil || ctx == nil {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l := b.lease
	l.mu.Lock()
	valid := l.self == l && !l.closing && !l.closed && !b.closed && l.borrowed == b
	prior := l.err
	l.mu.Unlock()
	if !valid {
		return errors.Join(os.ErrClosed, prior)
	}
	if prior != nil {
		return prior
	}
	held, err := l.file.Stat()
	original, oe := l.root.Stat(".")
	parent, pe := b.parent.Stat(".")
	if err != nil || oe != nil || pe != nil || !os.SameFile(held, l.identity) || !os.SameFile(held, original) || !os.SameFile(held, parent) {
		return errors.Join(errStorageCustody, err, oe, pe)
	}
	return ctx.Err()
}
func (b *storageCreatorBorrow) Close() error {
	if b == nil || b.self != b || b.lease == nil {
		return storagebudget.ErrAuthority
	}
	l := b.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if b.closed {
		return nil
	}
	if l.borrowed != b {
		return storagebudget.ErrAuthority
	}
	b.closed = true
	l.borrowed = nil
	return nil
}
func (l *storageCreatorLease) Close() error {
	if l == nil || l.self != l {
		return storagebudget.ErrAuthority
	}
	l.mu.Lock()
	if l.closed {
		prior := l.err
		l.mu.Unlock()
		return prior
	}
	l.closing = true
	if l.borrowed != nil || l.closeRunning {
		l.mu.Unlock()
		return storagebudget.ErrBusy
	}
	l.closeRunning = true
	file := l.file
	l.mu.Unlock()
	err := file.Close()
	l.mu.Lock()
	l.err = errors.Join(l.err, err)
	l.closed = true
	l.closeRunning = false
	result := l.err
	l.mu.Unlock()
	return result
}

// storageCooperatingExisting opens name, which another cooperating creator
// may have made while it holds parent's creator lock, without creating it:
// the folder found, checked as the creator checks it, or a worded busy
// refusal (409) where there is none yet. held is the lock refusal.
func storageCooperatingExisting(parent *os.Root, name string, held error) (_ *os.Root, made bool, err error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, storageLockBusy(held)
	}
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errStorageCustody
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, false, err
	}
	opened, oe := root.Stat(".")
	named, ne := parent.Lstat(name)
	if oe != nil || ne != nil || !os.SameFile(info, opened) || !os.SameFile(info, named) {
		return nil, false, errors.Join(errStorageCustody, oe, ne, root.Close())
	}
	return root, false, nil
}

// Ordinary cooperating creators use this exact absent-name interval. The new
// admitted action uses its own original issuer and no-replace/charged cuts.
// This helper grants no accounting permission or recovered creation receipt.
func storageCooperatingDirectory(ctx context.Context, parent *os.Root, name string, original *storageCreatorLease) (root *os.Root, made bool, err error) {
	if !storageOwnerCatalogNameComponent([]byte(name)) {
		return nil, false, storagebudget.ErrBounds
	}
	defer func() {
		if err != nil && root != nil {
			err = errors.Join(err, root.Close())
			root = nil
		}
	}()
	var l *storageCreatorLease
	owned := false
	if original != nil {
		held, he := original.file.Stat()
		named, ne := parent.Stat(".")
		if he != nil || ne != nil {
			return nil, false, errors.Join(he, ne)
		}
		if os.SameFile(held, named) {
			l = original
		}
	}
	if l == nil {
		l, err = storageCreatorAcquire(parent)
		if err != nil {
			if !lockHeld(err) {
				return nil, false, err
			}
			// Another cooperating creator holds the parent. It is never
			// waited for: a folder it has already made is used as found, as
			// an existing folder always is; otherwise the caller is refused,
			// worded, and may retry.
			return storageCooperatingExisting(parent, name, err)
		}
		owned = true
	}
	if owned {
		defer func() { err = errors.Join(err, l.Close()) }()
	}
	b, err := l.borrow(ctx, parent)
	if err != nil {
		return nil, false, err
	}
	defer func() { err = errors.Join(err, b.Close()) }()
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if err = parent.Mkdir(name, custodyDirMode); err == nil {
			made = true
		} else if errors.Is(err, os.ErrExist) {
			err = nil
		}
		if err == nil {
			info, err = parent.Lstat(name)
		}
	}
	if err != nil {
		return nil, made, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, made, errStorageCustody
	}
	if err = b.current(ctx); err != nil {
		return nil, made, err
	}
	root, err = parent.OpenRoot(name)
	if err != nil {
		return nil, made, err
	}
	opened, oe := root.Stat(".")
	named, ne := parent.Lstat(name)
	if oe != nil || ne != nil || !os.SameFile(info, opened) || !os.SameFile(info, named) {
		return root, made, errors.Join(errStorageCustody, oe, ne)
	}
	if err = b.current(ctx); err != nil {
		return root, made, err
	}
	return root, made, nil
}

func (o *storageBudgetOwner) creationTailReusable(r storageCreationIntent, page []byte, status storagebudget.HostState) bool {
	if o == nil || !o.valid() || r.validate() != nil || len(page) != storageIntentPage || !r.Core.reusable() {
		return false
	}
	o.ref.cell.mu.Lock()
	endpoint := o.ref.cell.enrollmentEndpoint
	catalog := o.ref.cell.ownerCatalog
	o.ref.cell.mu.Unlock()
	if !storageEnrollmentPeakCoreCatalog(r.Core, catalog) {
		return false
	}
	if r.Kind == storageCreationLocalPrivate {
		decoded, e := storageDecodeCreationIntent(page, int(r.Core.Slot), o.ref.cell.record.Installation, o.ref.cell.binding)
		if e != nil || !reflect.DeepEqual(decoded, r) || o.validateCreationReferencedComponents(r) != nil || r.Core.Phase != storageIntentRetired || !storageIntentRetiredProof(r.Core, status) && !o.coveredEnrollmentHost(storageIntentSlot{record: r.Core, page: page}, status) {
			return false
		}
		ticket := status.Slots[r.Core.Token.Slot].Ticket
		return ticket.Token != r.Core.Token || storageCreationTicketMatchesBound(r, ticket, o.ref.cell.binding) && ticket.Evidence == r.Core.Evidence && reflect.DeepEqual(ticket.Changes, r.Core.Changes)
	}
	host := storageIntentSlot{record: r.Core, page: page}
	switch r.Core.Phase {
	case storageIntentRejected:
		if status.Epoch != r.Core.AdmissionEpoch && (endpoint == nil || !storageEnrollmentPeakCoversHost(*endpoint, host, status)) {
			return false
		}
		for _, slot := range status.Slots {
			if storageIntentMatchingTicket(r.Core, slot.Ticket, o.ref.cell.binding) {
				return false
			}
		}
		return true
	case storageIntentRetired:
		return endpoint != nil && storageCreationTailIncluded(r, *endpoint) && storageEnrollmentPeakCoversHost(*endpoint, host, status)
	}
	return false
}

// Derive the retained tail from actual same-load originals. This returns data;
// the original action issuer must still check room, parent custody, selected
// workflow and native feature qualification before installing its reservation.
func (p *storageEnrollmentEpoch) originalCreationTail(ctx context.Context, bridge storagebudget.ID) (storageCreationTail, error) {
	var tail storageCreationTail
	if p == nil || p.self != p || p.original == nil || p.initialInspection == nil || p.qualified == nil || p.scope != nil || bridge == (storagebudget.ID{}) {
		return tail, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return tail, err
	}
	if err := p.qualified.current(ctx, p); err != nil {
		return tail, err
	}
	tail = storageCreationTail{Installation: p.c.ref.cell.ownerRecord.Installation, QBinding: p.c.ref.cell.ownerRecord.QBinding, Bridge: bridge, Config: p.c.ref.cell.ownerRecord.Config, Anchor: p.c.ref.cell.ownerRecord.Anchor, Project: p.original.Project, Qualification: p.qualified.digest, Targets: p.original.Targets, Before: p.original.Extents, MaximumExtent: storageEnrollmentPeakEvidenceMax}
	for i, raw := range p.original.Catalogs {
		tail.Catalogs[i] = sha256.Sum256(raw)
	}
	var err error
	tail.ParentPeaks, tail.Logical, err = storageCreationTailDimensions(p.previousEvidence != nil, tail.Before)
	if err != nil {
		return tail, err
	}
	var catalog storageOwnerCatalogV2
	if p.previousEvidence != nil {
		catalog = p.previousEvidence.Inner.Catalog
	} else {
		cores, _, e := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if e != nil {
			return tail, e
		}
		pair, e := storageOwnerDecodeCatalogPairV3(cores[0], cores[1], *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
		if e != nil {
			return tail, e
		}
		catalog = pair.High
	}
	rows := map[storagebudget.ID]*[80]byte{}
	for _, root := range catalog.Roots {
		if root.Role != 1 && root.Role != 2 {
			continue
		}
		parent := int(root.Role - 1)
		for i := 0; i < int(root.Definition.CapacityCount); i++ {
			ref := root.Definition.CapacityRefs[i]
			row := rows[ref.Constraint]
			if row == nil {
				row = new([80]byte)
				copy(row[:16], ref.Constraint[:])
				storageEnrollmentPeakPut64(row[16:24], ref.Generation)
				rows[ref.Constraint] = row
			}
			if row[24]&(1<<parent) != 0 || storageEnrollmentPeakU64(row[16:24]) != ref.Generation {
				return tail, storagebudget.ErrBounds
			}
			row[24] |= 1 << parent
			copy(row[48+parent*16:64+parent*16], root.Definition.Filesystem[:])
			amount := tail.ParentPeaks[parent]
			storageEnrollmentPeakPut64(row[32:40], storageEnrollmentPeakU64(row[32:40])+amount.Bytes)
			storageEnrollmentPeakPut64(row[40:48], storageEnrollmentPeakU64(row[40:48])+amount.Inodes)
		}
	}
	for _, row := range rows {
		tail.Capacity = append(tail.Capacity, *row)
	}
	sort.Slice(tail.Capacity, func(i, j int) bool { return bytes.Compare(tail.Capacity[i][:16], tail.Capacity[j][:16]) < 0 })
	if err = tail.validate(); err != nil {
		return tail, err
	}
	return tail, p.held(ctx)
}

// Q admits the application components itself. Before that call, include the
// retained enrollment logical growth in the same installation envelope. It has
// no workspace attribution and is not zero-settled when the action retires.
// Strict original inspection separately proves no outstanding Q promise.
func storageCreationLogicalRoom(tail storageCreationTail, components []storagebudget.Component, q *storageEnrollmentPeakQCheckpoint) error {
	if tail.validate() != nil || q == nil || storageEnrollmentPeakQValidateCheckpoint(q) != nil || q.Maintenance {
		return storagebudget.ErrBounds
	}
	usage := q.Installation
	for _, root := range q.Roots {
		if root.ID != (storageEnrollmentPeakQID{}) && !root.Known {
			return storagebudget.ErrUnknown
		}
	}
	for _, workspace := range q.Workspaces {
		if workspace.ID != (storageEnrollmentPeakQID{}) && !workspace.Known {
			return storagebudget.ErrUnknown
		}
	}
	for _, component := range components {
		if component.Category >= storagebudget.Categories {
			return storagebudget.ErrBounds
		}
		growth, err := storageVectorAdd(component.Physical, component.SettlementPhysical)
		if err != nil {
			return err
		}
		value, err := storageEnrollmentPeakQAdd(usage[component.Category], storageEnrollmentPeakQVector{Bytes: growth.Bytes, Files: growth.Files, Directories: growth.Directories})
		if err != nil {
			return err
		}
		usage[component.Category] = value
	}
	growth := storageEnrollmentPeakQVector{Bytes: tail.Logical.Bytes, Files: tail.Logical.Files, Directories: tail.Logical.Directories}
	var err error
	usage[storagebudget.Control], err = storageEnrollmentPeakQAdd(usage[storagebudget.Control], growth)
	if err != nil {
		return err
	}
	for category, value := range usage {
		if !storageLimitsAdvisory && !value.le(q.Policy.Installation.Category[category]) {
			return storagebudget.ErrLimit
		}
	}
	total, err := storageEnrollmentPeakQSum(usage)
	if err != nil {
		return err
	}
	if !storageLimitsAdvisory && !total.le(q.Policy.Installation.Total) {
		return storagebudget.ErrLimit
	}
	return nil
}

// Subtract the separately retained tail from fresh original observations before
// Q considers this action's own exact component promises. The caller owns use
// EX throughout; this does not mutate Q counters or manufacture a receipt.
func storageCreationCapacityRoom(tail storageCreationTail, observations []storagebudget.CapacityObservation) ([]storagebudget.CapacityObservation, error) {
	if tail.validate() != nil {
		return nil, storagebudget.ErrBounds
	}
	out := append([]storagebudget.CapacityObservation{}, observations...)
	for _, raw := range tail.Capacity {
		var id storagebudget.ID
		copy(id[:], raw[:16])
		found := -1
		for i, observation := range out {
			if observation.Constraint == id {
				if found >= 0 {
					return nil, storagebudget.ErrBounds
				}
				found = i
			}
		}
		if found < 0 {
			return nil, storagebudget.ErrUnknown
		}
		observation := &out[found]
		if observation.Generation != storageEnrollmentPeakU64(raw[16:24]) {
			return nil, storagebudget.ErrStale
		}
		bytes, inodes := storageEnrollmentPeakU64(raw[32:40]), storageEnrollmentPeakU64(raw[40:48])
		if observation.KnownResources&storagebudget.CapacityBytes != 0 {
			if observation.Available.Bytes < bytes {
				return nil, storagebudget.ErrHeadroom
			}
			observation.Available.Bytes -= bytes
		}
		if observation.KnownResources&storagebudget.CapacityInodes != 0 {
			if observation.Available.Inodes < inodes {
				return nil, storagebudget.ErrHeadroom
			}
			observation.Available.Inodes -= inodes
		}
	}
	return out, nil
}

// Complete manifest operands use the same direct final-name semantics selected
// by J4. Every effect has an entry, including zero-byte files and directories;
// no fake stage, omitted effect or path-based native identity is introduced.
func storageCreationRequest(r storageCreationIntent) (storagebudget.Request, error) {
	return storageCreationRequestBound(r, r.Tail.QBinding)
}
func storageCreationRequestBound(r storageCreationIntent, binding storagebudget.Digest) (storagebudget.Request, error) {
	if r.validate() != nil || binding == (storagebudget.Digest{}) || r.Kind != storageCreationLocalPrivate && binding != r.Tail.QBinding {
		return storagebudget.Request{}, storagebudget.ErrBounds
	}
	request := storagebudget.Request{Workflow: r.Core.Workflow, Operation: r.Core.Operation, Owner: binding, Components: append([]storagebudget.Component{}, r.Core.Components...), Entries: []storagebudget.Entry{}}
	names := make([]string, 0, len(r.Parents)+len(r.Effects))
	for _, parent := range r.Parents {
		prefix := ""
		if (r.Kind == storageCreationPrivate || r.Kind == storageCreationLocalPrivate) && parent.Role == 3 {
			prefix = "dialect-drafts"
		}
		names = append(names, prefix)
	}
	for _, effect := range r.Effects {
		parent := effect.Parent
		target := effect.Name
		if names[parent] != "" {
			target = names[parent] + "/" + target
		}
		for int(parent) >= len(r.Parents) {
			parent = r.Effects[int(parent)-len(r.Parents)].Parent
		}
		entry := storagebudget.Entry{Kind: storagebudget.ObjectEntry, Component: r.Parents[parent].Component, Target: target, Stage: target, NewIdentity: r.Core.CASBinding, Bytes: effect.Bytes}
		if effect.Kind == 1 {
			entry.Directories = 1
		} else {
			entry.Files = 1
		}
		request.Entries = append(request.Entries, entry)
		names = append(names, target)
	}
	return request, nil
}

// A single synchronous member of the existing original enrollment lifetime.
// Decoding J4 cannot construct this object. Parents and children below are
// actual retained descriptions, not paths/tuples promoted into ownership.
type storageCreationLive struct {
	self        *storageCreationLive
	phase       *storageEnrollmentEpoch
	requester   *Server // actual original server or its retained registered child
	record      storageCreationIntent
	request     storagebudget.Request
	parents     []*storageCreationLiveParent
	objects     [4]storageCreationLiveObject
	payloads    [4][]byte
	host        storageNamedFile // borrowed from the original construction
	selected    storageIntentSlot
	reservation *storageCreationLiveReservation
	hostWrites  int
	qCalls      [4]bool
	effectCalls [4][2]bool
	started     bool
	admitted    storagebudget.Token
	finished    bool
	recovering  bool
}
type storageCreationLiveReservation struct {
	member             *storageCreationLive
	tail               storageCreationTail
	originalCheckpoint storagebudget.Digest
	observations       []storagebudget.CapacityObservation
	qualification      storagebudget.Digest // actual original live profile; never serialized as a local tail
}
type storageCreationLiveParent struct {
	root    *os.Root
	binding *storageRootBinding
	file    *os.File
	project *storageEnrollmentEpoch // exact borrowed original project EX
	domain  *storageDomainHold      // exact borrowed original directory-domain EX
	lease   *storageCreatorLease    // owned independent parent EX, when no alias exists
	borrow  *storageCreatorBorrow
	edge    *storageNamedRoot // only the exact existing dialect-drafts descendant
}
type storageCreationLiveObject struct {
	parent   *os.Root
	name     string
	root     *os.Root // actual returned child handle retained under creator exclusion
	file     *os.File // actual O_EXCL descriptor, or child-directory EX descriptor
	identity os.FileInfo
	native   storageOwnerPersistentObject
	lease    *storageCreatorLease
	borrow   *storageCreatorBorrow
	source   *storageOriginalSourceRoot // same captured root transferred to original sources
}

func (m *storageCreationLive) current(ctx context.Context) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.self != m.phase || m.phase.creation != m || m.reservation == nil || m.reservation.member != m || !reflect.DeepEqual(m.reservation.tail, m.record.Tail) || m.phase.qualified == nil || m.phase.qualified.digest != m.reservation.qualification || m.record.Kind != storageCreationLocalPrivate && m.phase.qualified.digest != m.record.Tail.Qualification {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	if err := m.record.requireCapacityEstimate(); err != nil {
		return err
	}
	if err := m.requesterCurrent(ctx); err != nil {
		return err
	}
	if err := m.hostCurrent(); err != nil {
		return err
	}
	if err := m.record.validate(); err != nil {
		return err
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	if err := p.qualified.current(ctx, p); err != nil {
		return err
	}
	if len(m.parents) != len(m.record.Parents) {
		return storagebudget.ErrAuthority
	}
	files := make([]*os.File, len(m.parents))
	for i, parent := range m.parents {
		if parent == nil || parent.root == nil || parent.file == nil || parent.binding == nil {
			return storagebudget.ErrAuthority
		}
		if err := parent.binding.current(ctx); err != nil {
			return err
		}
		if parent.edge != nil {
			if err := parent.edge.current(); err != nil {
				return err
			}
		}
		switch {
		case parent.project != nil:
			if parent.project != p || parent.file != p.project {
				return storagebudget.ErrAuthority
			}
		case parent.domain != nil:
			found := false
			for _, hold := range p.holds {
				found = found || hold == parent.domain
			}
			if !found {
				return storagebudget.ErrAuthority
			}
			if err := parent.domain.current(ctx); err != nil {
				return err
			}
			actual := parent.domain
			for actual.borrowed != nil {
				actual = actual.borrowed
			}
			if parent.file != actual.file {
				return storagebudget.ErrAuthority
			}
		case parent.lease != nil:
			if parent.borrow == nil || parent.file != parent.lease.file {
				return storagebudget.ErrAuthority
			}
			if err := parent.borrow.current(ctx); err != nil {
				return err
			}
		default:
			return storagebudget.ErrAuthority
		}
		held, err := parent.file.Stat()
		named, e := parent.root.Stat(".")
		if err != nil || e != nil || !os.SameFile(held, named) {
			return errors.Join(errStorageCustody, err, e)
		}
		native, err := storageAdvisoryPersistentHeld(ctx, parent.file)
		if err != nil || native != m.record.Parents[i].Native {
			return errors.Join(errStorageCustody, err)
		}
		files[i] = parent.file
	}
	for i, effect := range m.record.Effects {
		if effect.State >= storageCreationCaptured {
			if err := m.objectCurrent(ctx, i, effect.State == storageCreationDurable); err != nil {
				return err
			}
		}
	}
	return p.qualified.creationParents(ctx, p, m.record.Kind, m.record.Branch, storageCreationOriginalRoles(m.record), files)
}
func (m *storageCreationLive) Close() (err error) {
	if m == nil {
		return nil
	}
	if m.self != m {
		return storagebudget.ErrAuthority
	}
	for i := len(m.objects) - 1; i >= 0; i-- {
		object := &m.objects[i]
		if object.borrow != nil {
			err = errors.Join(err, object.borrow.Close())
			object.borrow = nil
		}
		if object.lease != nil {
			err = errors.Join(err, object.lease.Close())
			object.lease = nil
			object.file = nil
		} else if object.file != nil {
			err = errors.Join(err, object.file.Close())
			object.file = nil
		}
		if object.root != nil && object.source == nil {
			err = errors.Join(err, object.root.Close())
			object.root = nil
		}
	}
	for i := len(m.parents) - 1; i >= 0; i-- {
		parent := m.parents[i]
		if parent.borrow != nil {
			err = errors.Join(err, parent.borrow.Close())
			parent.borrow = nil
		}
		if parent.lease != nil {
			err = errors.Join(err, parent.lease.Close())
			parent.lease = nil
			parent.file = nil
		}
		if parent.edge != nil {
			err = errors.Join(err, parent.edge.root.Close())
			parent.edge = nil
		}
	}
	return err
}

// This is the one allowed in-flight exception to an original implicit pointer
// observation. It requires this invocation's actual returned O_EXCL descriptor
// and retained settings parent. It grants neither native effects nor authority
// to replace an old explicit selection. It never accepts matching body/path as
// issuance. The ordinary original source checks remain in force around it.
func (m *storageCreationLive) pendingPointerCurrent(ctx context.Context, selection storageOriginalHistorySelection) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.creation != m || m.reservation == nil || m.reservation.member != m || selection.explicit || selection.location.Previous != "" {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	if err := p.permit(ctx); err != nil {
		return err
	}
	if !p.sourceHoldsComplete {
		return storagebudget.ErrAuthority
	}
	for i, effect := range m.record.Effects {
		if effect.Name != dataLocationName {
			continue
		}
		object := &m.objects[i]
		if object.parent != selection.settings || object.file == nil || object.identity == nil || object.native != (effect.Native) || !object.native.valid(2) {
			return errStorageCustody
		}
		info, err := object.file.Stat()
		named, e := object.parent.Lstat(object.name)
		if err != nil || e != nil || !storagePrivate(info, false) || !os.SameFile(info, object.identity) || !os.SameFile(info, named) || info.Size() < 0 || uint64(info.Size()) > effect.Bytes {
			return errors.Join(errStorageCustody, err, e)
		}
		witness := storageNamedFile{root: object.parent, name: object.name, file: object.file, identity: object.identity, size: info.Size()}
		native, err := storageOwnerPersistentFile(ctx, witness)
		if err != nil || native != object.native {
			return errors.Join(errStorageCustody, err)
		}
		if effect.State == storageCreationDurable {
			if uint64(info.Size()) != effect.Bytes {
				return errStorageCustody
			}
			raw := make([]byte, int(effect.Bytes))
			n, err := object.file.ReadAt(raw, 0)
			if err != nil || n != len(raw) || sha256.Sum256(raw) != effect.Payload {
				return errors.Join(errStorageCustody, err)
			}
		}
		return p.permit(ctx)
	}
	return errStorageCustody
}

func (p *storageEnrollmentEpoch) bindCreationParents(ctx context.Context, m *storageCreationLive) error {
	if p == nil || m == nil || m.phase != p || p.creation != nil || len(m.parents) != 0 {
		return storagebudget.ErrAuthority
	}
	r := m.record
	s := m.requester
	retainedLocal := p.startup && p.startupProject != nil && m.recovering && r.Kind == storageCreationLocalPrivate && s == nil
	if s == nil && !retainedLocal {
		return storagebudget.ErrAuthority
	}
	projectPath := p.originalProjectPath()
	if s != nil {
		projectPath = s.projectDir
	}
	for _, definition := range r.Parents {
		if retainedLocal && definition.Role != 2 && definition.Role != 3 {
			return storagebudget.ErrAuthority
		}
		var binding *storageRootBinding
		if definition.Component != 255 {
			if int(definition.Component) >= len(r.Core.Components) {
				return storagebudget.ErrBounds
			}
			component := r.Core.Components[definition.Component]
			binding = p.c.owner.roots[component.Root]
			if binding == nil || binding.row.Owner != component.Owner || binding.row.Generation != component.RootGeneration || binding.row.Filesystem != component.Filesystem || p.c.owner.filesystems[component.Filesystem].Generation != component.FilesystemGeneration || binding.row.Identity != r.Core.RootIdentities[definition.Component] || p.c.owner.filesystems[component.Filesystem].Identity != r.Core.FilesystemIdentities[definition.Component] || binding.row.Locator != r.Core.Locators[definition.Component] || !p.c.owner.intentCapacityMatches(binding, component) {
				return errStorageCustody
			}
		} else {
			for _, candidate := range p.originalRoots {
				if candidate.source != nil && candidate.source.persistent == definition.Native && (candidate.source.role == 4 || r.legacyConfigSelection() && candidate.configControl) {
					if binding != nil {
						return errStorageCustody
					}
					binding = candidate
				}
			}
		}
		if binding == nil || binding.source == nil {
			return errStorageCustody
		}
		if err := binding.current(ctx); err != nil {
			return err
		}
		parent := &storageCreationLiveParent{root: binding.root, binding: binding}
		m.parents = append(m.parents, parent)
		if definition.Role == 3 {
			if binding.source.path != filepath.Join(projectPath, ".desk-private") || binding.category != storagebudget.Control {
				return errStorageCustody
			}
			edge, err := storagePinChild(binding.root, "dialect-drafts", true)
			if err != nil {
				return err
			}
			parent.edge = &edge
			parent.root = edge.root
		}
		native, err := storageOwnerPersistentRoot(ctx, parent.root)
		if err != nil || native != definition.Native {
			return errors.Join(errStorageCustody, err)
		}
		if binding.transferInactive {
			return storagebudget.ErrAuthority
		}
		switch definition.Role {
		case 1:
			if native != p.original.Project || binding.source.role != 7 || binding.restrictedNamespace != ".desk-private" {
				return errStorageCustody
			}
		case 2:
			if binding.source.path != filepath.Join(projectPath, ".desk-private") || binding.category != storagebudget.Control {
				return errStorageCustody
			}
		case 3:
		case 4:
			expected := filepath.Dir(s.defaultChatDataPath())
			if binding.source.path != expected || r.Branch == storageCreationConfigured && parent.root != p.c.original.config || r.Branch == storageCreationContained && binding.source.path != filepath.Join(projectPath, ".desk-private") || r.Branch == storageCreationPersonal && binding.source.role != 8 {
				return errStorageCustody
			}
		case 5:
			expected := s.configDir
			if r.Branch == storageCreationContained {
				expected = filepath.Join(projectPath, ".desk-private")
			}
			if binding.source.path != expected {
				return errStorageCustody
			}
		case 6:
			if r.legacyConfigSelection() {
				if !binding.configControl || native != r.Tail.Config {
					return errStorageCustody
				}
			} else if binding.source.role != 4 || binding.source.path != r.SelectedPath {
				return errStorageCustody
			}
		case 7:
			if binding.source.role != 8 || binding.source.scope == nil || binding.source.scope.Default == nil || binding.source.relative != r.Core.Target || filepath.Join(binding.source.path, r.Core.Target) != r.SelectedPath {
				return errStorageCustody
			}
		default:
			return storagebudget.ErrBounds
		}
		// Reuse only a description proved to be the actual directory inode. The
		// alternative is an owned nonblocking EX; a pathname/role never means held.
		rootInfo, err := parent.root.Stat(".")
		if err != nil {
			return err
		}
		projectInfo, err := p.project.Stat()
		if err != nil {
			return err
		}
		if os.SameFile(rootInfo, projectInfo) {
			parent.project = p
			parent.file = p.project
			continue
		}
		for _, hold := range p.holds {
			if !hold.root.domainDirectory {
				continue
			}
			if err := hold.current(ctx); err != nil {
				return err
			}
			// A domain alias borrows the original EX and owns no File. Keep
			// the alias as authority while using its actual held description.
			actual := hold
			for actual.borrowed != nil {
				actual = actual.borrowed
			}
			if actual.file == nil {
				return storagebudget.ErrAuthority
			}
			info, e := actual.file.Stat()
			if e != nil {
				return e
			}
			if os.SameFile(rootInfo, info) {
				parent.domain = hold
				parent.file = actual.file
				break
			}
		}
		if parent.file != nil {
			continue
		}
		// Alias parents in the same action share one owned lease/borrow and Close.
		for _, old := range m.parents[:len(m.parents)-1] {
			if old.file == nil {
				continue
			}
			info, e := old.file.Stat()
			if e != nil {
				return e
			}
			if os.SameFile(rootInfo, info) {
				parent.lease = old.lease
				parent.borrow = old.borrow
				parent.file = old.file
				break
			}
		}
		if parent.file != nil {
			continue
		}
		parent.lease, err = storageCreatorAcquire(parent.root)
		if err != nil {
			return err
		}
		parent.file = parent.lease.file
		parent.borrow, err = parent.lease.borrow(ctx, parent.root)
		if err != nil {
			return err
		}
	}
	return p.held(ctx)
}

func (p *storageEnrollmentEpoch) issueOriginalCreation(ctx context.Context, proposed storageCreationIntent, payloads map[int][]byte) (out *storageCreationLive, err error) {
	if p == nil || p.c == nil {
		return nil, storagebudget.ErrAuthority
	}
	return p.issueOriginalCreationFor(ctx, p.c.server, proposed, payloads)
}
func (p *storageEnrollmentEpoch) issueOriginalCreationFor(ctx context.Context, requester *Server, proposed storageCreationIntent, payloads map[int][]byte) (out *storageCreationLive, err error) {
	if p == nil || p.self != p || p.original == nil || p.initialInspection == nil || p.qualified == nil || p.scope != nil || p.creation != nil {
		return nil, storagebudget.ErrAuthority
	}
	if err = p.held(ctx); err != nil {
		return nil, err
	}
	installation, binding := p.c.ref.cell.ownerRecord.Installation, p.c.ref.cell.binding
	var tail storageCreationTail
	if proposed.Kind != storageCreationLocalPrivate {
		bridge, e := storagebudget.NewID()
		if e != nil {
			return nil, e
		}
		tail, err = p.originalCreationTail(ctx, bridge)
		if err != nil {
			return nil, err
		}
	} else if p.qualified.record.Version < 2 {
		return nil, errStorageEnrollmentQualification
	}
	r := proposed
	// The original issuer chooses semantics; caller metadata grants nothing.
	r.CapacityPolicy = storageEnrollmentCapacityEstimatePolicy
	r.Tail = tail
	r.Core.Phase = storageIntentPrepared
	r.Core.Token = storagebudget.Token{}
	r.Core.PreviousToken = storagebudget.Token{}
	r.Core.PolicyRevision = 0
	r.Core.RetirementEpoch = 0
	r.Core.Evidence = storagebudget.Digest{}
	r.Core.Outcome = 0
	r.Core.Rejection = 0
	r.Core.AdmissionEpoch = p.initialInspection.View.Configuration.Epoch
	r.Core.Changes = make([]storagebudget.Change, len(r.Core.Components))
	r.Core.OperationID, err = storagebudget.NewID()
	if err != nil {
		return nil, err
	}
	chosen := -1
	var old storageIntentSlot
	table := storageIntentTable{owner: p.c.owner}
	for slot, pages := range p.original.Hosts {
		selected, e := storageSelectCreationHost(pages[0], pages[1], slot, installation, binding)
		if e != nil {
			return nil, e
		}
		if selected.Identity != nil || selected.PredecessorIdentity != nil {
			_, lineage, e := p.identityGraph()
			if e != nil {
				return nil, e
			}
			for _, identity := range []*storageIdentityIntent{selected.Identity, selected.PredecessorIdentity} {
				if identity == nil {
					continue
				}
				if e = p.identityComponentMappings(ctx, identity.Core, identity.Core.Components[0].Root); e != nil {
					return nil, e
				}
				raw, e := storageEncodeIdentityIntent(*identity, installation, binding)
				if e != nil || !p.c.owner.identityReceiptCurrent(ctx, *identity, raw, lineage, storageHostState(p.initialInspection.View)) {
					return nil, errors.Join(errStorageEnrollmentUnresolved, e)
				}
			}
		}
		table.slots[slot] = selected.Slot
		table.identity[slot] = selected.Identity
		if selected.Slot.record.Phase != storageIntentEmpty && !selected.Slot.record.reusable() {
			return nil, errStorageEnrollmentUnresolved
		}
		if chosen < 0 && (selected.Creation == nil || p.c.owner.creationTailCurrent(ctx, *selected.Creation, selected.Slot.page, storageHostState(p.initialInspection.View))) {
			chosen = slot
			old = selected.Slot
		}
	}
	if err = p.c.owner.validateIntentMappings(ctx, &table, storageHostState(p.initialInspection.View)); err != nil {
		return nil, err
	}
	if chosen < 0 {
		return nil, storagebudget.ErrBounds
	}
	r.Core.Slot = uint16(chosen)
	r.Core.Generation = 1
	r.Core.PreviousPageDigest = storagebudget.Digest{}
	if old.record.Phase != storageIntentEmpty {
		if old.record.Generation == ^uint64(0) {
			return nil, storagebudget.ErrBounds
		}
		r.Core.Generation = old.record.Generation + 1
		r.Core.PreviousPageDigest = sha256.Sum256(old.page)
	}
	r.Core.CASBinding = r.planDigest()
	r.Core.Operation = "p:" + hex.EncodeToString(r.Core.CASBinding[:]) + ":create"
	if err = r.validate(); err != nil {
		return nil, err
	}
	// Detach every slice before installing any original member. Caller DTOs
	// remain comparison input and cannot mutate the selected action afterward.
	encoded, err := storageEncodeCreationIntent(r, installation, binding)
	if err != nil {
		return nil, err
	}
	r, err = storageDecodeCreationIntent(encoded, int(r.Core.Slot), installation, binding)
	if err != nil {
		return nil, err
	}
	m := &storageCreationLive{phase: p, requester: requester, record: r, selected: old}
	m.self = m
	defer func() {
		if out == nil {
			err = errors.Join(err, m.Close())
		}
	}()
	if err = m.requesterCurrent(ctx); err != nil {
		return nil, err
	}
	if err = p.bindCreationParents(ctx, m); err != nil {
		return nil, err
	}
	files := make([]*os.File, len(m.parents))
	for i, parent := range m.parents {
		files[i] = parent.file
	}
	if err = p.qualified.creationParents(ctx, p, r.Kind, r.Branch, storageCreationOriginalRoles(r), files); err != nil {
		return nil, err
	}
	if err = m.validateOriginalSelection(ctx); err != nil {
		return nil, err
	}
	for i, effect := range r.Effects {
		if effect.State != storageCreationOwed || effect.Native != (storageOwnerPersistentObject{}) {
			return nil, storagebudget.ErrAuthority
		}
		if effect.Kind == 2 {
			body, present := payloads[i]
			if !present || uint64(len(body)) != effect.Bytes || sha256.Sum256(body) != effect.Payload {
				return nil, storagebudget.ErrBounds
			}
			m.payloads[i] = append([]byte{}, body...)
		} else if _, present := payloads[i]; present {
			return nil, storagebudget.ErrBounds
		}
		if int(effect.Parent) < len(m.parents) {
			if _, e := m.parents[effect.Parent].root.Lstat(effect.Name); !errors.Is(e, os.ErrNotExist) {
				return nil, errors.Join(errStorageCustody, e)
			}
		}
	}
	if len(payloads) > len(r.Effects) {
		return nil, storagebudget.ErrBounds
	}
	if err = m.validatePayloadsAndDemand(); err != nil {
		return nil, err
	}
	for _, control := range p.c.original.files {
		if control.root == p.c.original.config && control.name == storageIntentName {
			m.host = control
		}
	}
	if m.host.file == nil || m.host.size != storageIntentBytes {
		return nil, errStorageCustody
	}
	// Existing action controls are never densified. Their observed initialized,
	// allocated/unshared ranges must be covered by this exact installed profile.
	for _, control := range p.c.original.files {
		if control.name != storageIntentName && !(control.root == p.c.anchor && (control.name == "tickets.a" || control.name == "tickets.b" || control.name == "manifests")) {
			continue
		}
		if err = control.current(); err != nil {
			return nil, err
		}
		if err = storageEnrollmentNativeDense(ctx, control.file, uint64(control.size)); err != nil {
			return nil, err
		}
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(installation), Binding: storageEnrollmentPeakQDigest(binding)}
	q0, err := storageEnrollmentPeakQDecodeCheckpoint(append([]byte{}, p.initialInspection.Checkpoints[p.initialInspection.CheckpointBank]...), anchor)
	if err != nil {
		return nil, err
	}
	if r.Kind != storageCreationLocalPrivate {
		if err = storageCreationLogicalRoom(tail, r.Core.Components, q0); err != nil {
			return nil, err
		}
	}
	ids := map[storagebudget.ID]bool{}
	for _, raw := range tail.Capacity {
		var id storagebudget.ID
		copy(id[:], raw[:16])
		ids[id] = true
	}
	for _, component := range r.Core.Components {
		for i := 0; i < int(component.CapacityCount); i++ {
			ids[component.Capacity[i].Constraint] = true
		}
	}
	ordered := make([]storagebudget.ID, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i][:], ordered[j][:]) < 0 })
	observations, err := p.originalEnrollmentObservations(ctx, ordered)
	if err != nil {
		return nil, err
	}
	if r.Kind != storageCreationLocalPrivate {
		observations, err = storageCreationCapacityRoom(tail, observations)
		if err != nil {
			return nil, err
		}
	}
	m.request, err = storageCreationRequestBound(r, binding)
	if err != nil {
		return nil, err
	}
	m.reservation = &storageCreationLiveReservation{member: m, tail: tail, qualification: p.qualified.digest, originalCheckpoint: sha256.Sum256(p.initialInspection.Checkpoints[p.initialInspection.CheckpointBank]), observations: observations}
	if err = p.held(ctx); err != nil {
		return nil, err
	}
	p.creation = m
	if err = m.current(ctx); err != nil {
		return nil, err
	}
	if err = p.prepareCreationLaunch(ctx, m); err != nil {
		return nil, err
	}
	out = m
	return out, nil
}

func storageDefaultLegacyNames(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if conversationFileName.MatchString(entry.Name()) || assistantRunFileName.MatchString(entry.Name()) || assistantSourceFileName.MatchString(entry.Name()) {
			return true
		}
	}
	return false
}
func (m *storageCreationLive) validateOriginalSelection(ctx context.Context) error {
	if m == nil || m.self != m || m.phase == nil || len(m.parents) != len(m.record.Parents) {
		return storagebudget.ErrAuthority
	}
	p, r := m.phase, m.record
	s := m.requester
	retainedLocal := p.startup && p.startupProject != nil && m.recovering && r.Kind == storageCreationLocalPrivate && s == nil
	if s == nil && !retainedLocal {
		return storagebudget.ErrAuthority
	}
	projectPath := p.originalProjectPath()
	if s != nil {
		projectPath = s.projectDir
	}
	if err := p.held(ctx); err != nil {
		return err
	}
	switch r.Kind {
	case storageCreationPrivate, storageCreationLocalPrivate:
		if r.SelectedPath != filepath.Join(projectPath, ".desk-private", "dialect-drafts") {
			return storagebudget.ErrAuthority
		}
	case storageCreationAncestor:
		if r.Branch != storageCreationPersonal || s.containedDeskData() || s.cfg.DeskConfigDir != "" || len(m.parents) != 1 || m.parents[0].binding.source.scope == nil || !p.c.sources.historyAncestorSelected(ctx, *m.parents[0].binding.source.scope) {
			return storagebudget.ErrAuthority
		}
	case storageCreationHistory:
		branch := byte(storageCreationConfigured)
		if s.containedDeskData() {
			branch = storageCreationContained
		} else if s.cfg.DeskConfigDir == "" {
			branch = storageCreationPersonal
		}
		if r.Branch != branch {
			return storagebudget.ErrAuthority
		}
		var settings *os.Root
		for i, parent := range r.Parents {
			if parent.Role == 5 {
				settings = m.parents[i].root
			}
		}
		if settings == nil {
			return storagebudget.ErrAuthority
		}
		selected := false
		for _, selection := range p.c.sources.selections {
			if selection.settings == settings && !selection.explicit && selection.pending == nil && selection.location.Previous == "" && selection.location.Path == s.defaultChatDataPath() {
				selected = true
			}
		}
		if !selected {
			return errStorageCustody
		}
		if _, err := settings.Lstat(dataLocationName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
		entries, err := storageEntries(settings)
		if err != nil {
			return err
		}
		legacy := !s.containedDeskData() && storageDefaultLegacyNames(entries)
		if legacy != r.legacyConfigSelection() {
			return storagebudget.ErrStale
		}
		expected := s.defaultChatDataPath()
		if legacy {
			expected = s.configDir
		}
		if r.SelectedPath != expected {
			return storagebudget.ErrAuthority
		}
	default:
		return storagebudget.ErrAuthority
	}
	return p.held(ctx)
}
func (m *storageCreationLive) validatePayloadsAndDemand() error {
	r := m.record
	expected := make([]storagebudget.Vector, len(r.Core.Components))
	for i, effect := range r.Effects {
		parent := effect.Parent
		for int(parent) >= len(r.Parents) {
			parent = r.Effects[int(parent)-len(r.Parents)].Parent
		}
		index := int(r.Parents[parent].Component)
		if index >= len(expected) {
			return storagebudget.ErrBounds
		}
		amount := storagebudget.Vector{}
		if effect.Kind == 1 {
			amount.Directories = 1
		} else {
			amount.Bytes = effect.Bytes
			amount.Files = 1
		}
		var err error
		expected[index], err = storageVectorAdd(expected[index], amount)
		if err != nil {
			return err
		}
		if (r.Kind == storageCreationPrivate || r.Kind == storageCreationLocalPrivate) && effect.Kind == 2 {
			body := []byte{}
			switch effect.Name {
			case ".gitignore":
				body = []byte("*\n")
			case "workflow.lock":
			default:
				return storagebudget.ErrAuthority
			}
			if !bytes.Equal(body, m.payloads[i]) {
				return storagebudget.ErrAuthority
			}
		}
		if r.Kind == storageCreationHistory && effect.Kind == 2 {
			var body []byte
			switch effect.Name {
			case dataLocationName:
				body = m.requester.encodeDataLocation(dataLocation{Version: 1, Path: r.SelectedPath})
			case dataMarkerName:
				body, err = json.Marshal(dataMarker{Version: 1, Owner: m.requester.storageOwner()})
			default:
				return storagebudget.ErrAuthority
			}
			if err != nil || !bytes.Equal(body, m.payloads[i]) {
				return errors.Join(storagebudget.ErrAuthority, err)
			}
		}
	}
	for i, component := range r.Core.Components {
		attribution := storagebudget.Vector{}
		if component.Workspace != (storagebudget.ID{}) {
			attribution = expected[i]
		}
		if leader := storageCreationAttributionLeader(r.Core, i); leader >= 0 {
			attribution = expected[leader]
		}
		if component.Physical != expected[i] || component.Attribution != attribution || component.SettlementPhysical != (storagebudget.Vector{}) || component.SettlementAttribution != (storagebudget.Vector{}) {
			return storagebudget.ErrBounds
		}
	}
	return m.validateReferencedComponents()
}

// Persisted attribution rows must be exactly the original root's reference
// census. Component DTOs, matching locators and shared roots cannot select a
// smaller workspace set. Every expanded row still matches its original root,
// filesystem, capacities and complete identity operands independently.
func (m *storageCreationLive) validateReferencedComponents() error {
	return m.phase.c.owner.validateCreationReferencedComponents(m.record)
}
func (o *storageBudgetOwner) validateCreationReferencedComponents(r storageCreationIntent) error {
	used := make([]bool, len(r.Core.Components))
	groups := map[int]bool{}
	for _, parent := range r.Parents {
		if parent.Component != 255 {
			groups[int(parent.Component)] = true
		}
	}
	for index := range groups {
		if index >= len(r.Core.Components) {
			return storagebudget.ErrBounds
		}
		leader := r.Core.Components[index]
		binding := o.roots[leader.Root]
		if binding == nil {
			return errStorageCustody
		}
		refs := binding.references
		if binding.configControl && leader.Category == storagebudget.Control {
			refs = nil
		}
		expected := append([]storagebudget.ID{}, refs...)
		if len(expected) == 0 {
			expected = []storagebudget.ID{{}}
		}
		for j, id := range expected {
			at := index
			if j > 0 {
				at = -1
				for i, row := range r.Core.Components {
					if row.Workspace == id && storageCreationAttributionLeader(r.Core, i) == index {
						if at >= 0 {
							return storagebudget.ErrBounds
						}
						at = i
					}
				}
			}
			if at < 0 || used[at] || r.Core.Components[at].Workspace != id {
				return storagebudget.ErrBounds
			}
			used[at] = true
			row := r.Core.Components[at]
			fs, ok := o.filesystems[row.Filesystem]
			if !ok || row.Root != binding.row.ID || row.RootGeneration != binding.row.Generation || row.Owner != binding.row.Owner || row.Filesystem != binding.row.Filesystem || row.FilesystemGeneration != fs.Generation || r.Core.RootIdentities[at] != binding.row.Identity || r.Core.FilesystemIdentities[at] != fs.Identity || r.Core.Locators[at] != binding.row.Locator || !o.intentCapacityMatches(binding, row) {
				return errStorageCustody
			}
			if id != (storagebudget.ID{}) {
				w, ok := o.workspaces[id]
				if !ok || w.Generation != row.WorkspaceGeneration {
					return errStorageCustody
				}
			}
		}
	}
	for _, ok := range used {
		if !ok {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

// Creation uses the same entered-call drain as enrollment. Its pre-effect
// reservation is the original issuer's complete action plus retained tail;
// a decoded J4 page cannot enter this leaf.
func (m *storageCreationLive) enter(ctx context.Context) error {
	if err := m.current(ctx); err != nil {
		return err
	}
	p := m.phase
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.sourceHoldsComplete || !p.running || p.revoked || p.entered || p.finishing || p.err != nil || ctx.Err() != nil || p.ctx.Err() != nil {
		return storagebudget.ErrAuthority
	}
	p.entered = true
	return nil
}
func (m *storageCreationLive) next(phase storageIntentPhase) storageCreationIntent {
	r := m.record
	r.Core.Generation = m.selected.record.Generation + 1
	r.Core.PreviousPageDigest = sha256.Sum256(m.selected.page)
	r.Core.Phase = phase
	r.Core.Changes = append([]storagebudget.Change{}, r.Core.Changes...)
	r.Effects = append([]storageCreationEffect{}, r.Effects...)
	return r
}

// There are at most seven phase pages and two pages per native effect. The
// original host descriptor and complete other-bank image stay pinned; no old
// page is recovered from a hash or inferred from a phase number.
func (m *storageCreationLive) publish(ctx context.Context, r storageCreationIntent) (err error) {
	if m == nil || m.self != m || m.hostWrites >= 7+2*len(m.record.Effects) || r.validate() != nil {
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
		prior, e := storageDecodeCreationIntent(old.page, int(r.Core.Slot), m.phase.c.ref.cell.ownerRecord.Installation, m.phase.c.ref.cell.binding)
		if e != nil || !storageCreationIntentTransition(prior, r, old.page) {
			return errors.Join(storagebudget.ErrStale, e)
		}
	}
	raw, err := storageEncodeCreationIntent(r, m.phase.c.ref.cell.ownerRecord.Installation, m.phase.c.ref.cell.binding)
	if err != nil {
		return err
	}
	if err = m.takeLaunch(ctx, uint16(m.hostWrites+1)); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	defer func() { err = m.phase.leaveNative(err) }()
	m.hostWrites++
	m.started = true
	if err = m.host.current(); err != nil {
		return err
	}
	// Check all64 original mappings immediately before every host overwrite.
	actual := make([]byte, storageIntentBytes)
	n, e := m.host.file.ReadAt(actual, 0)
	if e != nil || n != len(actual) {
		return errors.Join(errStorageCustody, e)
	}
	for slot, pages := range m.phase.original.Hosts {
		for bank, page := range pages {
			at := (slot*2 + bank) * storageIntentPage
			if !bytes.Equal(actual[at:at+storageIntentPage], page) {
				return errStorageCustody
			}
		}
	}
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
	offset := int64((int(r.Core.Slot)*2 + 1 - old.bank) * storageIntentPage)
	if err = m.phase.boundary("creation-host-write"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeWrite(ctx, write.file, raw, offset); err != nil {
		return err
	}
	if err = m.phase.boundary("creation-host-sync"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeSync(ctx, write.file); err != nil {
		return err
	}
	closed = true
	err = errors.Join(write.file.Close(), m.phase.boundary("creation-host-close"))
	if err != nil {
		return err
	}
	read, err := storageOpenFile(m.host.root, m.host.name, m.host.size)
	if err != nil {
		return err
	}
	back := make([]byte, storageIntentPage)
	n, e = read.file.ReadAt(back, offset)
	err = errors.Join(e, read.current(), read.file.Close(), m.phase.boundary("creation-host-readback"))
	if err != nil || n != len(back) || !bytes.Equal(back, raw) {
		return errors.Join(errStorageCustody, err)
	}
	if err = m.host.current(); err != nil {
		return err
	}
	m.record = r
	m.selected = storageIntentSlot{record: r.Core, page: raw, bank: 1 - old.bank}
	m.phase.original.Hosts[r.Core.Slot][m.selected.bank] = append([]byte{}, raw...)
	if r.Kind == storageCreationLocalPrivate {
		point := ""
		if r.Core.Phase == storageIntentPublishing {
			point = "local-private-all-durable"
		} else if r.Core.Phase == storageIntentRetired {
			point = "local-private-retired"
		} else if r.Core.Phase == storageIntentStaging {
			for _, effect := range r.Effects {
				if effect.State == storageCreationCaptured {
					point = "local-private-effect-captured"
					break
				}
			}
		}
		if point != "" {
			if err = m.phase.boundary(point); err != nil {
				return err
			}
		}
	}
	return m.phase.permit(ctx)
}

func storageCreationTicketMatches(r storageCreationIntent, t storagebudget.Ticket) bool {
	return storageCreationTicketMatchesBound(r, t, r.Tail.QBinding)
}
func storageCreationTicketMatchesBound(r storageCreationIntent, t storagebudget.Ticket, binding storagebudget.Digest) bool {
	return binding != (storagebudget.Digest{}) && (r.Kind == storageCreationLocalPrivate || binding == r.Tail.QBinding) && t.State != storagebudget.Empty && t.Workflow == r.Core.Workflow && t.Operation == r.Core.Operation && t.Owner == binding && reflect.DeepEqual(t.Components, r.Core.Components)
}
func (m *storageCreationLive) ticket(ctx context.Context, token storagebudget.Token, state storagebudget.State) (storagebudget.Ticket, error) {
	var empty storagebudget.Ticket
	if err := m.current(ctx); err != nil {
		return empty, err
	}
	status, err := m.phase.c.ref.cell.q.InspectHostState(ctx)
	if err != nil {
		return empty, err
	}
	if status.Epoch != m.record.Core.AdmissionEpoch || int(token.Slot) >= len(status.Slots) {
		return empty, errStorageEnrollmentUnresolved
	}
	t := status.Slots[token.Slot].Ticket
	if !storageCreationTicketMatchesBound(m.record, t, m.request.Owner) || t.Token != token || t.State != state {
		return empty, errStorageEnrollmentUnresolved
	}
	if err = storageOriginalActionRequest(ctx, m.phase.c.ref.cell.q, m.record.Core, m.request, status, token); err != nil {
		return empty, err
	}
	return t, m.current(ctx)
}
func (m *storageCreationLive) takeQ(ctx context.Context, index int) error {
	if index < 0 || index >= len(m.qCalls) || m.qCalls[index] {
		return storagebudget.ErrAuthority
	}
	ids := [][]uint16{{20, 21}, {22}, {23, 24}, {25}}[index]
	for _, id := range ids {
		if err := m.takeLaunch(ctx, id); err != nil {
			return err
		}
	}
	if err := m.enter(ctx); err != nil {
		return err
	}
	m.qCalls[index] = true
	return nil
}

// Reserve and Advance are each invoked once, only after the corresponding
// complete original host publication. A recovered Prepared/Reserved is never
// fed into this live leaf, even when current Q still looks like actual-before.
func (m *storageCreationLive) admit(ctx context.Context) (err error) {
	if m == nil || m.self != m || m.started || m.record.Core.Phase != storageIntentPrepared {
		return storagebudget.ErrAuthority
	}
	if err = m.validateOriginalSelection(ctx); err != nil {
		return err
	}
	comparison, err := m.phase.c.owner.captureOriginalReserve(ctx, m.request, nil, m.record.Core.OperationID, nil)
	if err != nil {
		return err
	}
	if err = m.publish(ctx, m.record); err != nil {
		return err
	}
	if err = m.takeQ(ctx, 0); err != nil {
		return err
	}
	token, e := m.phase.c.ref.cell.q.ReserveCompared(ctx, m.request, m.reservation.observations, comparison)
	err = m.phase.leaveNative(e)
	if err != nil {
		return err
	}
	m.admitted = token
	ticket, err := m.ticket(ctx, token, storagebudget.Reserved)
	if err != nil {
		return err
	}
	r := m.next(storageIntentReserved)
	r.Core.Token = token
	r.Core.PolicyRevision = ticket.PolicyRevision
	if err = m.publish(ctx, r); err != nil {
		return err
	}
	if err = m.takeQ(ctx, 1); err != nil {
		return err
	}
	advanced, e := m.phase.c.ref.cell.q.Advance(ctx, token, m.request.Owner, storagebudget.Staging, r.Core.CASBinding)
	err = m.phase.leaveNative(e)
	if err != nil {
		return err
	}
	m.admitted = advanced
	ticket, err = m.ticket(ctx, advanced, storagebudget.Staging)
	if err != nil || ticket.PolicyRevision != r.Core.PolicyRevision || ticket.Evidence != r.Core.CASBinding {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	r = m.next(storageIntentStaging)
	r.Core.PreviousToken = token
	r.Core.Token = advanced
	r.Core.Evidence = r.Core.CASBinding
	return m.publish(ctx, r)
}

func (m *storageCreationLive) effectParent(index int) (*os.Root, *os.File, error) {
	if index < 0 || index >= len(m.record.Effects) {
		return nil, nil, storagebudget.ErrBounds
	}
	parent := int(m.record.Effects[index].Parent)
	if parent < len(m.parents) {
		return m.parents[parent].root, m.parents[parent].file, nil
	}
	at := parent - len(m.parents)
	if at >= index || m.record.Effects[at].Kind != 1 || m.record.Effects[at].State != storageCreationDurable || m.objects[at].root == nil || m.objects[at].file == nil {
		return nil, nil, storagebudget.ErrAuthority
	}
	return m.objects[at].root, m.objects[at].file, nil
}
func (m *storageCreationLive) objectCurrent(ctx context.Context, index int, exact bool) error {
	if index < 0 || index >= len(m.record.Effects) {
		return storagebudget.ErrBounds
	}
	effect := m.record.Effects[index]
	object := &m.objects[index]
	if effect.State < storageCreationCaptured || object.file == nil || object.parent == nil || object.identity == nil || object.native != effect.Native {
		return errStorageCustody
	}
	held, err := object.file.Stat()
	named, e := object.parent.Lstat(object.name)
	if err != nil || e != nil || !os.SameFile(held, object.identity) || !os.SameFile(held, named) || held.Mode()&os.ModeSymlink != 0 {
		return errors.Join(errStorageCustody, err, e)
	}
	var native storageOwnerPersistentObject
	if effect.Kind == 1 {
		if !storagePrivate(held, true) || object.root == nil || object.borrow == nil {
			return errStorageCustody
		}
		if err = object.borrow.current(ctx); err != nil {
			return err
		}
		native, err = storageAdvisoryPersistentHeld(ctx, object.file)
	} else {
		if !storagePrivate(held, false) || held.Size() < 0 || uint64(held.Size()) > effect.Bytes || exact && uint64(held.Size()) != effect.Bytes {
			return errStorageCustody
		}
		native, err = storageOwnerPersistentFile(ctx, storageNamedFile{root: object.parent, name: object.name, file: object.file, identity: object.identity, size: held.Size()})
		if err == nil && exact {
			body := make([]byte, int(effect.Bytes))
			n, e := object.file.ReadAt(body, 0)
			if e != nil || n != len(body) || sha256.Sum256(body) != effect.Payload {
				return errors.Join(errStorageCustody, e)
			}
		}
	}
	if err != nil || native != effect.Native {
		return errors.Join(errStorageCustody, err)
	}
	return nil
}
func (m *storageCreationLive) attachPendingPointer(index int) error {
	if m.record.Effects[index].Name != dataLocationName {
		return nil
	}
	object := &m.objects[index]
	found := 0
	for i := range m.phase.c.sources.selections {
		selection := &m.phase.c.sources.selections[i]
		if selection.settings != object.parent {
			continue
		}
		if selection.explicit || selection.pending != nil {
			return errStorageCustody
		}
		selection.pending = m
		found++
	}
	if found != 1 {
		return errStorageCustody
	}
	return nil
}

// This one no-replace call is never retried. The returned descriptors are
// installed before every fallible capture. A death before the Captured page
// remains Owed/uncertain on disk; matching names cannot complete that cut.
func (m *storageCreationLive) captureEffect(ctx context.Context, index int) (err error) {
	if m == nil || m.self != m || m.record.Core.Phase != storageIntentStaging || index < 0 || index >= len(m.record.Effects) || m.record.Effects[index].State != storageCreationOwed || m.effectCalls[index][0] {
		return storagebudget.ErrAuthority
	}
	for i := 0; i < index; i++ {
		if m.record.Effects[i].State != storageCreationDurable {
			return storagebudget.ErrAuthority
		}
	}
	if _, err = m.ticket(ctx, m.record.Core.Token, storagebudget.Staging); err != nil {
		return err
	}
	parent, parentFile, err := m.effectParent(index)
	if err != nil {
		return err
	}
	if err = m.phase.qualified.privateEffectParent(ctx, m.phase, m.record.Kind, parentFile); err != nil {
		return err
	}
	if err = m.takeLaunch(ctx, m.effectLaunchGroup(index, false)); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	m.effectCalls[index][0] = true
	defer func() { err = m.phase.leaveNative(err) }()
	effect := m.record.Effects[index]
	object := &m.objects[index]
	object.parent = parent
	object.name = effect.Name
	if _, e := parent.Lstat(effect.Name); !errors.Is(e, os.ErrNotExist) {
		return errors.Join(errStorageCustody, e)
	}
	if err = m.phase.boundary("creation-effect-create"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if effect.Kind == 1 {
		if err = parent.Mkdir(effect.Name, custodyDirMode); err != nil {
			return err
		}
		object.root, err = parent.OpenRoot(effect.Name)
		if err != nil {
			return err
		}
		object.lease, err = storageCreatorAcquire(object.root)
		if err != nil {
			return err
		}
		object.file = object.lease.file
		object.borrow, err = object.lease.borrow(ctx, object.root)
		if err != nil {
			return err
		}
	} else {
		object.file, err = parent.OpenFile(effect.Name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow, custodyFileMode)
		if err != nil {
			return err
		}
	}
	object.identity, err = object.file.Stat()
	if err != nil {
		return err
	}
	if effect.Kind == 1 {
		object.native, err = storageAdvisoryPersistentHeld(ctx, object.file)
	} else {
		object.native, err = storageOwnerPersistentFile(ctx, storageNamedFile{root: parent, name: effect.Name, file: object.file, identity: object.identity, size: 0})
	}
	if err != nil {
		return err
	}
	// Install only this invocation's actual capture, before checking original
	// currentness that now sees the exact initial pointer name.
	r := m.next(storageIntentStaging)
	r.Effects[index].State = storageCreationCaptured
	r.Effects[index].Native = object.native
	m.record = r
	if err = m.attachPendingPointer(index); err != nil {
		return err
	}
	if err = m.objectCurrent(ctx, index, false); err != nil {
		return err
	}
	if err = m.phase.boundary("creation-effect-captured"); err != nil {
		return err
	}
	return m.phase.permit(ctx)
}

func (m *storageCreationLive) persistEffect(ctx context.Context, index int) (err error) {
	if m == nil || m.self != m || m.record.Core.Phase != storageIntentStaging || index < 0 || index >= len(m.record.Effects) || m.record.Effects[index].State != storageCreationCaptured || m.effectCalls[index][1] {
		return storagebudget.ErrAuthority
	}
	if err = m.objectCurrent(ctx, index, false); err != nil {
		return err
	}
	parent, parentFile, err := m.effectParent(index)
	if err != nil {
		return err
	}
	if err = m.phase.qualified.privateEffectParent(ctx, m.phase, m.record.Kind, parentFile); err != nil {
		return err
	}
	if err = m.takeLaunch(ctx, m.effectLaunchGroup(index, true)); err != nil {
		return err
	}
	if err = m.enter(ctx); err != nil {
		return err
	}
	m.effectCalls[index][1] = true
	defer func() { err = m.phase.leaveNative(err) }()
	effect := m.record.Effects[index]
	object := &m.objects[index]
	var writer *os.File
	if effect.Kind == 1 {
		writer, err = object.root.Open(".")
	} else {
		writer, err = parent.OpenFile(effect.Name, os.O_RDWR|openNoFollow, 0)
	}
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, writer.Close())
		}
	}()
	info, err := writer.Stat()
	if err != nil || !os.SameFile(info, object.identity) {
		return errors.Join(errStorageCustody, err)
	}
	if effect.Kind == 2 {
		if err = m.phase.boundary("creation-effect-write"); err != nil {
			return err
		}
		if err = m.phase.permit(ctx); err != nil {
			return err
		}
		if len(m.payloads[index]) != 0 {
			if err = storageEnrollmentNativeWrite(ctx, writer, m.payloads[index], 0); err != nil {
				return err
			}
		}
	}
	if err = m.phase.boundary("creation-effect-sync"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeSync(ctx, writer); err != nil {
		return err
	}
	closed = true
	err = errors.Join(writer.Close(), m.phase.boundary("creation-effect-close"))
	if err != nil {
		return err
	}
	parentWriter, err := parent.Open(".")
	if err != nil {
		return err
	}
	parentClosed := false
	defer func() {
		if !parentClosed {
			err = errors.Join(err, parentWriter.Close())
		}
	}()
	held, err := parentWriter.Stat()
	original, e := object.parent.Stat(".")
	if err != nil || e != nil || !os.SameFile(held, original) {
		return errors.Join(errStorageCustody, err, e)
	}
	if err = m.phase.boundary("creation-parent-sync"); err != nil {
		return err
	}
	if err = m.phase.permit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentNativeSync(ctx, parentWriter); err != nil {
		return err
	}
	parentClosed = true
	err = errors.Join(parentWriter.Close(), m.phase.boundary("creation-parent-close"))
	if err != nil {
		return err
	}
	if err = m.objectCurrent(ctx, index, true); err != nil {
		return err
	}
	if err = m.phase.boundary("creation-effect-durable"); err != nil {
		return err
	}
	return m.phase.permit(ctx)
}

func storageCreationSettlement(r storageCreationIntent) (storageCreationIntent, error) {
	r.Core.Changes = make([]storagebudget.Change, len(r.Core.Components))
	for _, effect := range r.Effects {
		if effect.State != storageCreationDurable {
			return r, storagebudget.ErrAuthority
		}
		parent := effect.Parent
		for int(parent) >= len(r.Parents) {
			parent = r.Effects[int(parent)-len(r.Parents)].Parent
		}
		index := r.Parents[parent].Component
		amount := storagebudget.Vector{}
		if effect.Kind == 1 {
			amount.Directories = 1
		} else {
			amount.Bytes = effect.Bytes
			amount.Files = 1
		}
		var err error
		r.Core.Changes[index].PhysicalAdd, err = storageVectorAdd(r.Core.Changes[index].PhysicalAdd, amount)
		if err != nil {
			return r, err
		}
		if r.Core.Components[index].Workspace != (storagebudget.ID{}) {
			r.Core.Changes[index].AttributionAdd = r.Core.Changes[index].PhysicalAdd
		}
	}
	for i := range r.Core.Components {
		if leader := storageCreationAttributionLeader(r.Core, i); leader >= 0 {
			r.Core.Changes[i].AttributionAdd = r.Core.Changes[leader].PhysicalAdd
		}
	}
	r.Core.Outcome = storageIntentPublished
	factual := r
	factual.Core.Evidence = storagebudget.Digest{}
	factual.Core.Token = storagebudget.Token{}
	factual.Core.PreviousToken = storagebudget.Token{}
	factual.Core.PreviousPageDigest = storagebudget.Digest{}
	factual.Core.Generation = 0
	factual.Core.Phase = 0
	factual.Core.RetirementEpoch = 0
	var err error
	r.Core.Evidence, err = storageOwnerDigest("desk-monotone-creation-factual-settlement-v1", factual)
	return r, err
}

// This executes only a fresh admitted action. Retirement incorporates actual
// nonnegative deltas; the separately retained tail is still outstanding and
// keeps this host unavailable until the original Completed enrollment covers it.
func (m *storageCreationLive) execute(ctx context.Context) (err error) {
	if m.recovering {
		if err = m.persistOriginalLocal(ctx); err != nil {
			return err
		}
	} else if err = m.admit(ctx); err != nil {
		return err
	}
	for i := range m.record.Effects {
		if m.record.Effects[i].State == storageCreationOwed {
			if err = m.captureEffect(ctx, i); err != nil {
				return err
			}
			if err = m.publish(ctx, m.record); err != nil {
				return err
			}
		}
		if m.record.Effects[i].State == storageCreationCaptured {
			if err = m.persistEffect(ctx, i); err != nil {
				return err
			}
			r := m.next(storageIntentStaging)
			r.Effects[i].State = storageCreationDurable
			if err = m.publish(ctx, r); err != nil {
				return err
			}
		}
	}
	if m.record.Core.Phase == storageIntentStaging {
		if err = m.publish(ctx, m.next(storageIntentPublishing)); err != nil {
			return err
		}
	}
	if m.record.Core.Phase == storageIntentPublishing {
		r, e := storageCreationSettlement(m.next(storageIntentSettling))
		if e != nil {
			return e
		}
		if err = m.publish(ctx, r); err != nil {
			return err
		}
	}
	if m.record.Core.Phase == storageIntentSettling {
		r := m.record
		if err = m.takeQ(ctx, 2); err != nil {
			return err
		}
		terminal, e := m.phase.c.ref.cell.q.Settle(ctx, storagebudget.SettlementFact{Token: r.Core.Token, Owner: m.request.Owner, Evidence: r.Core.Evidence, Changes: r.Core.Changes})
		err = m.phase.leaveNative(e)
		if err != nil {
			return err
		}
		m.admitted = terminal
		ticket, e := m.ticket(ctx, terminal, storagebudget.Terminal)
		if e != nil || ticket.Evidence != r.Core.Evidence || !reflect.DeepEqual(ticket.Changes, r.Core.Changes) {
			return errors.Join(errStorageEnrollmentUnresolved, e)
		}
		m.phase.markApplication(m.record.Core.OperationID, m.launchAction(), true, false)
		next := m.next(storageIntentSettled)
		next.Core.PreviousToken = r.Core.Token
		next.Core.Token = terminal
		if err = m.publish(ctx, next); err != nil {
			return err
		}
	}
	if m.record.Core.Phase == storageIntentSettled {
		if err = m.takeQ(ctx, 3); err != nil {
			return err
		}
		e := m.phase.c.ref.cell.q.Checkpoint(ctx)
		err = m.phase.leaveNative(e)
		if err != nil {
			return err
		}
		if err = m.phase.boundary("creation-checkpoint-before-retired"); err != nil {
			return err
		}
		status, e := m.phase.c.ref.cell.q.InspectHostState(ctx)
		if e != nil {
			return e
		}
		r := m.next(storageIntentRetired)
		row := status.Slots[r.Core.Token.Slot]
		if status.Epoch != r.Core.AdmissionEpoch || row.PersistedApplied != r.Core.Token || row.PersistedNextSequence <= r.Core.Token.Sequence {
			return errStorageEnrollmentUnresolved
		}
		r.Core.RetirementEpoch = status.Epoch
		if err = m.publish(ctx, r); err != nil {
			return err
		}
	}
	if m.record.Core.Phase != storageIntentRetired {
		return storagebudget.ErrAuthority
	}
	m.phase.markApplication(m.record.Core.OperationID, m.launchAction(), true, false)
	if m.record.Kind == storageCreationLocalPrivate {
		if err = m.captureRetiredOriginal(ctx); err != nil {
			return err
		}
		m.finished = true
		m.phase.markApplication(m.record.Core.OperationID, m.launchAction(), true, true)
	}
	return nil
}

// Continue the same original lifetime after this member's factual retirement.
// The host banks were retained after each successful native publication. Only
// actual same-load Q checkpoint/ticket observations change here; no created
// directory or pointer is reopened and promoted into original custody.
func (m *storageCreationLive) captureRetiredOriginal(ctx context.Context) error {
	if m == nil || m.self != m || m.record.Core.Phase != storageIntentRetired || m.reservation == nil || m.reservation.member != m {
		return storagebudget.ErrAuthority
	}
	p := m.phase
	if p == nil || p.creation != m || p.original == nil || p.initialInspection == nil || p.scope != nil || p.initialPacket != nil {
		return storagebudget.ErrAuthority
	}
	if sha256.Sum256(p.initialInspection.Checkpoints[p.initialInspection.CheckpointBank]) != m.reservation.originalCheckpoint {
		return storagebudget.ErrStale
	}
	for _, called := range m.qCalls {
		if !called {
			return storagebudget.ErrAuthority
		}
	}
	for i, effect := range m.record.Effects {
		if effect.State != storageCreationDurable || !m.effectCalls[i][0] || !m.effectCalls[i][1] {
			return storagebudget.ErrAuthority
		}
	}
	if err := m.current(ctx); err != nil {
		return err
	}
	// The retained catalog pair, Ready and normal project still name the exact
	// pre-action originals. This action is not allowed to publish any of them.
	native, err := storageOwnerPersistentRoot(ctx, p.originalProjectRoot())
	if err != nil || native != p.original.Project {
		return errors.Join(errStorageCustody, err)
	}
	for target := byte(5); target <= 6; target++ {
		original := p.targets[target-1]
		if err = original.named.current(); err != nil {
			return err
		}
		native, err = storageOwnerPersistentFile(ctx, original.named)
		if err != nil || native != p.original.Targets[target-1] {
			return errors.Join(errStorageCustody, err)
		}
		raw := make([]byte, storageOwnerCatalogBytes)
		n, err := original.named.file.ReadAt(raw, 0)
		if err != nil || n != len(raw) || !bytes.Equal(raw, p.original.Catalogs[target-5]) {
			return errors.Join(errStorageCustody, err)
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
	selected, err := storageSelectCreationHost(p.original.Hosts[m.record.Core.Slot][0], p.original.Hosts[m.record.Core.Slot][1], int(m.record.Core.Slot), p.c.ref.cell.ownerRecord.Installation, p.c.ref.cell.binding)
	if err != nil || selected.Creation == nil || selected.Slot.bank != m.selected.bank || !bytes.Equal(selected.Slot.page, m.selected.page) || !reflect.DeepEqual(*selected.Creation, m.record) {
		return errors.Join(errStorageCustody, err)
	}
	q, err := p.c.ref.cell.q.InspectEnrollment(ctx)
	if err != nil || q.View.Configuration.Maintenance || q.View.Configuration.Epoch != m.record.Core.RetirementEpoch {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	_, after, err := storageOwnerActualDefinitions(q.View, p.c.ref.cell.ownerCatalog)
	if err != nil || !after {
		return errors.Join(errStorageCustody, err)
	}
	if m.record.Core.Token.Slot >= 64 {
		return storagebudget.ErrBounds
	}
	applied := q.View.Slots[m.record.Core.Token.Slot]
	if applied.PersistedApplied != m.record.Core.Token || applied.PersistedNextSequence <= m.record.Core.Token.Sequence {
		return errStorageEnrollmentUnresolved
	}
	for bank := 0; bank < 2; bank++ {
		raw := make([]byte, storagebudget.CheckpointBytes)
		n, err := p.targets[6+bank].named.file.ReadAt(raw, 0)
		if err != nil || n != len(raw) || !bytes.Equal(raw, q.Checkpoints[bank]) {
			return errors.Join(errStorageCustody, err)
		}
		if err = p.targets[6+bank].named.current(); err != nil {
			return err
		}
	}
	table := storageIntentTable{owner: p.c.owner}
	for slot, pages := range p.original.Hosts {
		choice, err := storageSelectCreationHost(pages[0], pages[1], slot, p.c.ref.cell.ownerRecord.Installation, p.c.ref.cell.binding)
		if err != nil {
			return err
		}
		if !choice.Slot.record.reusable() {
			return errStorageEnrollmentUnresolved
		}
		table.slots[slot] = choice.Slot
		table.identity[slot] = choice.Identity
	}
	if err = p.c.owner.validateIntentMappings(ctx, &table, storageHostState(q.View)); err != nil {
		return err
	}
	if err = m.current(ctx); err != nil {
		return err
	}
	p.original.Checkpoints = q.Checkpoints
	p.original.Tickets = q.Tickets
	p.initialInspection = &q
	return p.held(ctx)
}

func storageCreationOriginalQMapping(r storageCreationIntent, t storagebudget.Ticket, binding storagebudget.Digest) bool {
	if r.validate() != nil || !storageCreationTicketMatchesBound(r, t, binding) || r.Core.PolicyRevision != t.PolicyRevision {
		return false
	}
	c := r.Core
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

func (m *storageCreationLive) hostCurrent() error {
	if m.host.file == nil {
		return storagebudget.ErrAuthority
	}
	raw := make([]byte, storageIntentBytes)
	n, err := m.host.file.ReadAt(raw, 0)
	if err != nil || n != len(raw) {
		return errors.Join(errStorageCustody, err)
	}
	for slot, pages := range m.phase.original.Hosts {
		for bank, page := range pages {
			at := (slot*2 + bank) * storageIntentPage
			if !bytes.Equal(raw[at:at+storageIntentPage], page) {
				return errStorageCustody
			}
		}
	}
	return m.host.current()
}

func storageCreationEstimatePage(raw []byte) bool {
	return len(raw) == storageIntentPage && (string(raw[:8]) == "DSKHINT6" || string(raw[:8]) == "DSKHINT7")
}

func (o *storageBudgetOwner) creationTailCurrent(ctx context.Context, r storageCreationIntent, page []byte, status storagebudget.HostState) bool {
	return o.creationTailCurrentOriginal(ctx, r, page, status, nil)
}

func (o *storageBudgetOwner) creationTailCurrentOriginal(ctx context.Context, r storageCreationIntent, page []byte, status storagebudget.HostState, original *storagePolicyOriginal) bool {
	if o.creationTailReusable(r, page, status) {
		return true
	}
	if r.Kind != storageCreationLocalPrivate || r.validate() != nil || r.Core.Phase != storageIntentRetired || o.validateCreationReferencedComponents(r) != nil {
		return false
	}
	decoded, err := storageDecodeCreationIntent(page, int(r.Core.Slot), o.ref.cell.record.Installation, o.ref.cell.binding)
	if err != nil || !reflect.DeepEqual(decoded, r) || !o.policyCompletedHostOriginal(ctx, storageIntentSlot{record: r.Core, page: page}, status, original) {
		return false
	}
	ticket := status.Slots[r.Core.Token.Slot].Ticket
	return ticket.Token != r.Core.Token || storageCreationTicketMatchesBound(r, ticket, o.ref.cell.binding) && ticket.Evidence == r.Core.Evidence && reflect.DeepEqual(ticket.Changes, r.Core.Changes)
}
