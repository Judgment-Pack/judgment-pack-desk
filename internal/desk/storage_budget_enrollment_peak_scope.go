package desk

import (
	"bytes"
	"math"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageEnrollmentCapacityEstimateVersion = 3
	storageEnrollmentCapacityEstimatePolicy  = 2
)

type storageEnrollmentPeakScope [4096]byte

// Layout and evidence semantics are separate. Version3 retains all original-
// peer proofs from version2, under an explicitly different capacity policy.
func (s storageEnrollmentPeakScope) hasOriginalPeer() bool {
	version := storageEnrollmentPeakU16(s[:2])
	return version == 2 || version == storageEnrollmentCapacityEstimateVersion
}

func (s storageEnrollmentPeakScope) capacityEstimate() bool {
	return storageEnrollmentPeakU16(s[:2]) == storageEnrollmentCapacityEstimateVersion && s[2] == storageEnrollmentCapacityEstimatePolicy
}

// Parsing old debt never grants continuation under the revised estimate policy.
// Callers reconstruct its complete original reservation before this refusal.
func (s storageEnrollmentPeakScope) requireCapacityEstimate() error {
	if !s.capacityEstimate() {
		return errStorageEnrollmentPeakMigration
	}
	return nil
}

type storageEnrollmentPeakGroup struct {
	ID                                     uint16
	Parent, Target                         uint8
	Bytes, Files, Directories, MetadataOps uint64
	Completion                             uint16
	State                                  uint8
	Native                                 storageOwnerPersistentObject
	Expected                               storagebudget.Digest
	Generation                             uint64
}

func (s storageEnrollmentPeakScope) path() byte { return s[3] }
func (s storageEnrollmentPeakScope) final(bank byte) uint64 {
	return storageEnrollmentPeakU64(s[344+8*int(bank):])
}
func (s storageEnrollmentPeakScope) target(n byte) storageOwnerPersistentObject {
	if n == 9 {
		return storageEnrollmentPeakNative(s[216:251])
	}
	if n == 10 {
		return storageEnrollmentPeakNative(s[251:286])
	}
	if n < 1 || n > 8 {
		return storageOwnerPersistentObject{}
	}
	return storageEnrollmentPeakNative(s[1080+35*int(n-1) : 1080+35*int(n)])
}
func (s storageEnrollmentPeakScope) policyDigest() storagebudget.Digest {
	b := s
	clear(b[184:216])
	b[321] = 0
	clear(b[1080:1220])
	domain := "desk-enrollment-control-peak-v1"
	if s.hasOriginalPeer() {
		domain = "desk-enrollment-control-peak-v2"
	}
	if s.capacityEstimate() {
		domain = "desk-enrollment-control-capacity-estimate-v1"
	}
	return storageEnrollmentPeakHash(domain, b[:])
}

// Plan derives bounded structural operands only. Its diagnostic rows never
// constitute an unused permit or authorize replay after a restart.
// A schedule shape contains dimensions only. It has no installation, epoch,
// native-capture claim, census or checksum, and cannot be decoded as a Scope.
// The shared schedule also bounds a still-unissued creation enrollment tail.
type storageEnrollmentPeakScheduleShape struct {
	Path, CatalogBank, EvidenceBank, DispatchBank, CheckpointBank byte
	Final                                                         [2]uint64
	Targets                                                       [10]storageOwnerPersistentObject
}

func storageEnrollmentPeakPlan(s storageEnrollmentPeakScope) []storageEnrollmentPeakGroup {
	v := storageEnrollmentPeakScheduleShape{Path: s.path(), CatalogBank: s[322], EvidenceBank: s[323], DispatchBank: s[324], CheckpointBank: s[325], Final: [2]uint64{s.final(0), s.final(1)}}
	for i := range v.Targets {
		v.Targets[i] = s.target(byte(i + 1))
	}
	return storageEnrollmentPeakPlanShape(v)
}

func storageEnrollmentPeakPlanShape(v storageEnrollmentPeakScheduleShape) []storageEnrollmentPeakGroup {
	var ids []uint16
	switch v.Path {
	case 1:
		ids = []uint16{74, 75, 76, 77, 78, 79, 60, 80, 61, 81, 62, 82, 63, 83, 64, 84, 65, 85, 66, 86, 70, 71, 72, 73, 1, 2, 40, 3, 4, 41, 16, 51, 17, 52, 5, 6, 42, 11, 46, 12, 47, 18, 53, 21, 56, 19, 54, 22, 57, 13, 48, 20, 55, 23, 58, 7, 8, 43, 14, 49, 15, 50}
	case 2:
		ids = []uint16{70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 9, 44, 10, 45, 16, 51, 5, 6, 42, 11, 46, 12, 47, 18, 53, 21, 56, 19, 54, 22, 57, 13, 48, 20, 55, 23, 58, 7, 8, 43, 14, 49, 15, 50}
	case 3:
		ids = []uint16{70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 9, 44, 10, 45, 1, 2, 40, 67, 87, 68, 88, 3, 4, 41, 16, 51, 5, 6, 42, 11, 46, 12, 47, 18, 53, 21, 56, 19, 54, 22, 57, 13, 48, 20, 55, 23, 58, 7, 8, 43, 14, 49, 15, 50}
	default:
		return nil
	}
	c, e, d, q := v.CatalogBank, v.EvidenceBank, v.DispatchBank, v.CheckpointBank
	target := map[uint16]byte{}
	if v.Path == 1 {
		target[1] = 1
		target[3] = 2
		target[5] = 1
		target[7] = 2
		target[11] = 5 + 1 - c
		target[12] = 5 + c
		target[13] = 5 + c
		target[14] = 5 + 1 - c
		target[15] = 5 + c
		target[16] = 3
		target[17] = 4
		target[18] = 4
		target[19] = 3
		target[20] = 4
	} else {
		if v.Path == 2 {
			target[5] = 1 + 1 - e
			target[7] = 1 + e
			target[11] = 5 + c
			target[12] = 5 + 1 - c
			target[13] = 5 + 1 - c
			target[14] = 5 + c
			target[15] = 5 + 1 - c
		} else {
			target[1] = 1 + 1 - e
			target[3] = 1 + e
			target[5] = 1 + e
			target[7] = 1 + 1 - e
			target[11] = 5 + 1 - c
			target[12] = 5 + c
			target[13] = 5 + c
			target[14] = 5 + 1 - c
			target[15] = 5 + c
			target[67] = 5 + c
			target[68] = 5 + 1 - c
		}
		target[9] = 5 + 1 - c
		target[10] = 5 + c
		target[16] = 3 + 1 - d
		target[18] = 3 + d
		target[19] = 3 + 1 - d
		target[20] = 3 + d
	}
	target[21] = 7 + 1 - q
	target[22] = 7 + q
	target[23] = 7 + 1 - q
	target[60] = 5 + 1 - c
	target[61] = 5 + c
	target[66] = 5 + c
	for i := uint16(62); i <= 65; i++ {
		target[i] = byte(i - 61)
	}
	for i := uint16(70); i <= 79; i++ {
		target[i] = byte(i - 69)
	}
	completions := map[uint16]uint16{1: 40, 2: 40, 3: 41, 4: 41, 5: 42, 6: 42, 7: 43, 8: 43, 9: 44, 10: 45, 11: 46, 12: 47, 13: 48, 14: 49, 15: 50, 16: 51, 17: 52, 18: 53, 19: 54, 20: 55, 21: 56, 22: 57, 23: 58, 60: 80, 61: 81, 62: 82, 63: 83, 64: 84, 65: 85, 66: 86, 67: 87, 68: 88}
	for i := uint16(2); i <= 8; i += 2 {
		target[i] = target[i-1]
	}
	prior := map[uint16]storageEnrollmentPeakGroup{}
	out := make([]storageEnrollmentPeakGroup, 0, len(ids))
	for _, id := range ids {
		g := storageEnrollmentPeakGroup{ID: id, MetadataOps: 16, Completion: 65535}
		if old, ok := prior[id]; ok {
			g.Parent = old.Parent
			g.Target = old.Target
			g.Native = old.Native
		} else {
			g.Target = target[id]
			g.Parent = 1
			if g.Target == 7 || g.Target == 8 || g.Target == 10 {
				g.Parent = 2
			}
			g.Native = v.Targets[g.Target-1]
			switch {
			case id >= 1 && id <= 8:
				extent := v.Final[g.Target-1]
				if id%2 == 1 {
					g.Bytes = extent
					if g.Bytes > storageEnrollmentPeakChunk {
						g.Bytes = storageEnrollmentPeakChunk
					}
				} else {
					if extent <= storageEnrollmentPeakChunk {
						continue
					}
					g.Bytes = extent - storageEnrollmentPeakChunk
				}
			case id == 9 || id == 10 || id >= 11 && id <= 15 || id == 60 || id == 61 || id == 66 || id == 67 || id == 68:
				g.Bytes = 8388608
			case id >= 16 && id <= 20:
				g.Bytes = 4096
			case id >= 21 && id <= 23:
				g.Bytes = 4194304
				g.MetadataOps = 28
			case id >= 62 && id <= 65:
				g.Files = 1
			}
			if completion, ok := completions[id]; ok {
				g.Completion = completion
				prior[completion] = g
			}
		}
		out = append(out, g)
	}
	return out
}

func storageEnrollmentPeakEstimate(g storageEnrollmentPeakGroup) (storagebudget.CapacityVector, bool) {
	if g.Bytes > math.MaxUint64-65535 || g.Files > math.MaxUint64-g.Directories {
		return storagebudget.CapacityVector{}, false
	}
	units := (g.Bytes + 65535) / 65536
	n := g.Files + g.Directories
	if n > math.MaxUint64-g.MetadataOps || n+g.MetadataOps > math.MaxUint64-4 {
		return storagebudget.CapacityVector{}, false
	}
	m := n + g.MetadataOps + 4
	if units > math.MaxUint64/131072 || m > math.MaxUint64/65536 || units*131072 > math.MaxUint64-m*65536 || n > math.MaxUint64-2 {
		return storagebudget.CapacityVector{}, false
	}
	return storagebudget.CapacityVector{Bytes: units*131072 + m*65536, Inodes: n + 2}, true
}

func storageEnrollmentPeakDecodeScope(raw []byte) (storageEnrollmentPeakScope, error) {
	return storageEnrollmentPeakDecodeScopeVariant(raw, false)
}

// Only the fixed composition Snapshot4/Evidence2 context may retain a
// physical bank0 tie for duplicate founding V3 cores. Legacy Scope selection
// remains unchanged. The physical projection must separately prove that bank.
func storageEnrollmentPeakDecodeScopeVariant(raw []byte, composition bool) (storageEnrollmentPeakScope, error) {
	var s storageEnrollmentPeakScope
	if len(raw) != len(s) {
		return s, storagebudget.ErrBounds
	}
	copy(s[:], raw)
	fail := func() (storageEnrollmentPeakScope, error) {
		return storageEnrollmentPeakScope{}, storagebudget.ErrBounds
	}
	if (storageEnrollmentPeakU16(s[:2]) != 1 && !s.hasOriginalPeer()) || (storageEnrollmentPeakU16(s[:2]) == storageEnrollmentCapacityEstimateVersion && s[2] != storageEnrollmentCapacityEstimatePolicy || storageEnrollmentPeakU16(s[:2]) != storageEnrollmentCapacityEstimateVersion && s[2] != 1) || s.path() < 1 || s.path() > 3 || storageEnrollmentPeakU32(s[4:8]) != 7 || s[321] != 0 && s[321] != 15 || s[322] > 1 || s[325] > 1 || s[326] == 0 || s[326] > 8 || s[327] != 0 || storageEnrollmentPeakU64(s[400:408]) != 134217728 || storageEnrollmentPeakU16(s[1634:1636]) != 2 || storageEnrollmentPeakU32(s[1636:1640]) != 31 || !storageEnrollmentPeakOriginalPeerTailValidVariant(s, composition) {
		return fail()
	}
	for _, p := range [][2]int{{8, 24}, {24, 40}, {40, 56}, {56, 88}, {88, 120}, {120, 152}, {152, 184}, {408, 440}} {
		if storageEnrollmentPeakZero(s[p[0]:p[1]]) {
			return fail()
		}
	}
	for _, p := range [][2]int{{216, 251}, {251, 286}, {286, 321}} {
		if !storageEnrollmentPeakNative(s[p[0]:p[1]]).valid(1) {
			return fail()
		}
	}
	before := [2]uint64{storageEnrollmentPeakU64(s[328:336]), storageEnrollmentPeakU64(s[336:344])}
	final := [2]uint64{s.final(0), s.final(1)}
	if s.path() == 1 {
		if s[323] != 255 || s[324] != 255 || before != [2]uint64{} {
			return fail()
		}
	} else {
		if s[323] > 1 || s[324] > 1 || s[321] != 15 {
			return fail()
		}
	}
	for i := 0; i < 2; i++ {
		if final[i] == 0 || final[i] > storageEnrollmentPeakEvidenceMax || final[i]%65536 != 0 || before[i] > final[i] || before[i]%65536 != 0 || s.path() != 1 && before[i] == 0 {
			return fail()
		}
	}
	if s.path() == 2 && before != final || s.path() == 3 && before == final {
		return fail()
	}
	seq := storageEnrollmentPeakU64(s[1624:1632])
	if seq == 0 || seq > math.MaxUint64-5 || storageEnrollmentPeakU64(s[1616:1624]) == 0 {
		return fail()
	}
	natives := map[storageOwnerPersistentObject]bool{}
	for i := byte(1); i <= 8; i++ {
		p := s.target(i)
		if i <= 4 && s[321] == 0 {
			if p != (storageOwnerPersistentObject{}) {
				return fail()
			}
		} else {
			if !p.valid(2) || natives[p] {
				return fail()
			}
			natives[p] = true
		}
	}
	plan := storageEnrollmentPeakPlan(s)
	if len(plan) == 0 || len(plan) > 62 || int(storageEnrollmentPeakU16(s[1632:1634])) != len(plan) {
		return fail()
	}
	var parent [2]storagebudget.CapacityVector
	var attributed [10]uint64
	for _, g := range plan {
		v, ok := storageEnrollmentPeakEstimate(g)
		if !ok {
			return fail()
		}
		parent[g.Parent-1].Bytes += v.Bytes
		parent[g.Parent-1].Inodes += v.Inodes
		attributed[g.Target-1] += v.Bytes
	}
	if storageEnrollmentPeakU64(s[384:392]) != parent[0].Bytes+parent[1].Bytes || storageEnrollmentPeakU64(s[392:400]) != parent[0].Inodes+parent[1].Inodes {
		return fail()
	}
	growth := final[0] - before[0] + final[1] - before[1]
	files := uint64(0)
	if s.path() == 1 {
		growth += 8192
		files = 4
	}
	if storageEnrollmentPeakU64(s[360:368]) != growth || storageEnrollmentPeakU64(s[368:376]) != files || storageEnrollmentPeakU64(s[376:384]) != 0 {
		return fail()
	}
	for i := 0; i < 8; i++ {
		b := s[1360+i*32 : 1360+(i+1)*32]
		wantParent := byte(1)
		if i >= 6 {
			wantParent = 2
		}
		capture := byte(0)
		if s.path() == 1 && i < 4 {
			capture = 1
		}
		bef, fin := uint64(0), uint64(0)
		switch {
		case i < 2:
			bef = before[i]
			fin = final[i]
		case i < 4:
			fin = 4096
			if s.path() != 1 {
				bef = 4096
			}
		case i < 6:
			bef = 8388608
			fin = 8388608
		default:
			bef = 4194304
			fin = 4194304
		}
		if b[0] != wantParent || b[1] != byte(i+1) || b[2] != capture || b[3] != 0 || storageEnrollmentPeakU64(b[4:12]) != bef || storageEnrollmentPeakU64(b[12:20]) != fin || storageEnrollmentPeakU64(b[20:28]) != attributed[i] || storageEnrollmentPeakU32(b[28:32]) != 0 {
			return fail()
		}
	}
	var count [2]int
	var fs [2]storagebudget.ID
	for i := 0; i < 8; i++ {
		b := s[440+i*80 : 440+(i+1)*80]
		if i >= int(s[326]) {
			if !storageEnrollmentPeakZero(b) {
				return fail()
			}
			continue
		}
		if storageEnrollmentPeakZero(b[:16]) || storageEnrollmentPeakU64(b[16:24]) == 0 || b[24] < 1 || b[24] > 3 || !storageEnrollmentPeakZero(b[25:32]) || i > 0 && bytes.Compare(s[440+(i-1)*80:456+(i-1)*80], b[:16]) >= 0 {
			return fail()
		}
		want := storagebudget.CapacityVector{}
		for p := 0; p < 2; p++ {
			rawFS := b[48+p*16 : 64+p*16]
			if b[24]&(1<<p) != 0 {
				count[p]++
				if count[p] > 4 || storageEnrollmentPeakZero(rawFS) {
					return fail()
				}
				var id storagebudget.ID
				copy(id[:], rawFS)
				if fs[p] != (storagebudget.ID{}) && fs[p] != id {
					return fail()
				}
				fs[p] = id
				want.Bytes += parent[p].Bytes
				want.Inodes += parent[p].Inodes
			} else if !storageEnrollmentPeakZero(rawFS) {
				return fail()
			}
		}
		if storageEnrollmentPeakU64(b[32:40]) != want.Bytes || storageEnrollmentPeakU64(b[40:48]) != want.Inodes {
			return fail()
		}
	}
	if count[0] == 0 || count[1] == 0 {
		return fail()
	}
	digest := s.policyDigest()
	if !bytes.Equal(s[184:216], digest[:]) {
		return fail()
	}
	return s, nil
}

func storageEnrollmentPeakCaptureMatches(before, after storageEnrollmentPeakScope) bool {
	if before.path() != 1 || before[321] != 0 || after[321] != 15 {
		return false
	}
	a, b := before, after
	clear(a[1080:1220])
	clear(b[1080:1220])
	a[321] = 0
	b[321] = 0
	return a == b
}

// Check the union against full retained original rows, not caller estimates.
func (s storageEnrollmentPeakScope) checkCapacity(c storageOwnerCatalogV2) bool {
	var roots [2]*storageOwnerRoot
	for i := range c.Roots {
		r := &c.Roots[i]
		if r.Role == 1 {
			roots[0] = r
		}
		if r.Role == 2 {
			roots[1] = r
		}
	}
	if roots[0] == nil || roots[1] == nil {
		return false
	}
	for i, role := range []byte{13, 14} {
		found := false
		for _, o := range c.Objects {
			if o.Role == role {
				found = o.Persistent == s.target(byte(7+i))
			}
		}
		if !found {
			return false
		}
	}
	want := map[storagebudget.ID]struct {
		generation uint64
		mask       byte
		fs         [2]storagebudget.ID
	}{}
	for p, r := range roots {
		if int(r.DirectoryObject) >= len(c.Objects) || c.Objects[r.DirectoryObject].Persistent != s.target(byte(9+p)) || r.Definition.CapacityCount == 0 || r.Definition.CapacityCount > 4 {
			return false
		}
		for i := 0; i < int(r.Definition.CapacityCount); i++ {
			ref := r.Definition.CapacityRefs[i]
			v := want[ref.Constraint]
			if v.generation != 0 && v.generation != ref.Generation {
				return false
			}
			v.generation = ref.Generation
			v.mask |= 1 << p
			v.fs[p] = r.Definition.Filesystem
			want[ref.Constraint] = v
			found := false
			for _, row := range c.Constraints {
				if row.Definition.ID == ref.Constraint && row.Definition.Generation == ref.Generation {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	if len(want) != int(s[326]) {
		return false
	}
	for i := 0; i < int(s[326]); i++ {
		b := s[440+i*80 : 440+(i+1)*80]
		var id storagebudget.ID
		copy(id[:], b[:16])
		v, ok := want[id]
		if !ok || v.generation != storageEnrollmentPeakU64(b[16:24]) || v.mask != b[24] || !bytes.Equal(v.fs[0][:], b[48:64]) || !bytes.Equal(v.fs[1][:], b[64:80]) {
			return false
		}
	}
	return true
}
