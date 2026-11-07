package desk

// Handwritten wire/semantic fixtures. Expected bytes are assembled independently
// with standard LE/JSON primitives and the existing handwritten old-catalog
// material oracle; none are captured from this codec or an installed program.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type epBytes struct{ bytes.Buffer }

func (w *epBytes) fixed(v any) {
	if err := binary.Write(&w.Buffer, binary.LittleEndian, v); err != nil {
		panic(err)
	}
}
func (w *epBytes) data(v []byte) { w.fixed(uint16(len(v))); w.Write(v) }
func epJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func epHash(domain string, b []byte) storagebudget.Digest {
	return sha256.Sum256(append(append([]byte(domain), 0), b...))
}
func epNative(n uint64, kind byte) storageOwnerPersistentObject {
	return storageOwnerPersistentObject{Backend: 1, VolumeUUID: [16]byte{1}, Object: n, IncarnationKind: 1, ObjectKind: kind}
}
func epSet16(b []byte, n uint16) { binary.LittleEndian.PutUint16(b, n) }
func epSet32(b []byte, n uint32) { binary.LittleEndian.PutUint32(b, n) }
func epSet64(b []byte, n uint64) { binary.LittleEndian.PutUint64(b, n) }
func epAtNative(b []byte, p storageOwnerPersistentObject) {
	w := epBytes{}
	w.fixed(p)
	copy(b, w.Bytes())
}
func epFrame(magic string, payload []byte, seq uint64, full bool) []byte {
	n := len(payload) + 64
	if full {
		n = 8388608
	}
	b := make([]byte, n)
	copy(b, magic)
	epSet32(b[8:], 1)
	epSet32(b[12:], 64)
	epSet64(b[16:], seq)
	epSet32(b[24:], uint32(len(payload)))
	copy(b[64:], payload)
	d := sha256.Sum256(append(append([]byte{}, b[:32]...), payload...))
	copy(b[32:64], d[:])
	return b
}
func epScopeHash(s *storageEnrollmentPeakScope) {
	b := *s
	clear(b[184:216])
	b[321] = 0
	clear(b[1080:1220])
	d := epHash("desk-enrollment-control-peak-v1", b[:])
	copy(s[184:216], d[:])
}

// This independent plan table supplies the fixture schedule and operands. It
// does not call Plan/Estimate to construct an expected value.
func epGroups(path byte, extent [2]uint64, c, e, d, q byte) []storageEnrollmentPeakGroup {
	var ids []uint16
	switch path {
	case 1:
		ids = []uint16{74, 75, 76, 77, 78, 79, 60, 80, 61, 81, 62, 82, 63, 83, 64, 84, 65, 85, 66, 86, 70, 71, 72, 73, 1, 2, 40, 3, 4, 41, 16, 51, 17, 52, 5, 6, 42, 11, 46, 12, 47, 18, 53, 21, 56, 19, 54, 22, 57, 13, 48, 20, 55, 23, 58, 7, 8, 43, 14, 49, 15, 50}
	case 2:
		ids = []uint16{70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 9, 44, 10, 45, 16, 51, 5, 6, 42, 11, 46, 12, 47, 18, 53, 21, 56, 19, 54, 22, 57, 13, 48, 20, 55, 23, 58, 7, 8, 43, 14, 49, 15, 50}
	case 3:
		ids = []uint16{70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 9, 44, 10, 45, 1, 2, 40, 67, 87, 68, 88, 3, 4, 41, 16, 51, 5, 6, 42, 11, 46, 12, 47, 18, 53, 21, 56, 19, 54, 22, 57, 13, 48, 20, 55, 23, 58, 7, 8, 43, 14, 49, 15, 50}
	}
	targets := map[uint16]byte{21: 7 + 1 - q, 22: 7 + q, 23: 7 + 1 - q, 60: 5 + 1 - c, 61: 5 + c, 66: 5 + c, 62: 1, 63: 2, 64: 3, 65: 4, 9: 5 + 1 - c, 10: 5 + c, 67: 5 + c, 68: 5 + 1 - c}
	if path == 1 {
		for k, v := range map[uint16]byte{1: 1, 3: 2, 5: 1, 7: 2, 11: 5 + 1 - c, 12: 5 + c, 13: 5 + c, 14: 5 + 1 - c, 15: 5 + c, 16: 3, 17: 4, 18: 4, 19: 3, 20: 4} {
			targets[k] = v
		}
	} else {
		targets[16] = 3 + 1 - d
		targets[18] = 3 + d
		targets[19] = 3 + 1 - d
		targets[20] = 3 + d
		if path == 2 {
			for k, v := range map[uint16]byte{5: 1 + 1 - e, 7: 1 + e, 11: 5 + c, 12: 5 + 1 - c, 13: 5 + 1 - c, 14: 5 + c, 15: 5 + 1 - c} {
				targets[k] = v
			}
		} else {
			for k, v := range map[uint16]byte{1: 1 + 1 - e, 3: 1 + e, 5: 1 + e, 7: 1 + 1 - e, 11: 5 + 1 - c, 12: 5 + c, 13: 5 + c, 14: 5 + 1 - c, 15: 5 + c} {
				targets[k] = v
			}
		}
	}
	for i := uint16(2); i <= 8; i += 2 {
		targets[i] = targets[i-1]
	}
	for i := uint16(70); i <= 79; i++ {
		targets[i] = byte(i - 69)
	}
	completions := map[uint16]uint16{1: 40, 2: 40, 3: 41, 4: 41, 5: 42, 6: 42, 7: 43, 8: 43, 9: 44, 10: 45, 11: 46, 12: 47, 13: 48, 14: 49, 15: 50, 16: 51, 17: 52, 18: 53, 19: 54, 20: 55, 21: 56, 22: 57, 23: 58, 60: 80, 61: 81, 62: 82, 63: 83, 64: 84, 65: 85, 66: 86, 67: 87, 68: 88}
	saved := map[uint16]storageEnrollmentPeakGroup{}
	out := []storageEnrollmentPeakGroup{}
	for _, id := range ids {
		g := storageEnrollmentPeakGroup{ID: id, Parent: 1, Target: targets[id], MetadataOps: 16, Completion: 65535, Generation: 1}
		if old, ok := saved[id]; ok {
			g.Parent = old.Parent
			g.Target = old.Target
		} else {
			if g.Target == 7 || g.Target == 8 || g.Target == 10 {
				g.Parent = 2
			}
			switch {
			case id >= 1 && id <= 8:
				n := extent[g.Target-1]
				if id%2 == 1 {
					if n > 35651584 {
						n = 35651584
					}
				} else {
					if n <= 35651584 {
						continue
					}
					n -= 35651584
				}
				g.Bytes = n
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
			if v, ok := completions[id]; ok {
				g.Completion = v
				saved[v] = g
			}
		}
		out = append(out, g)
	}
	return out
}
func epEstimate(g storageEnrollmentPeakGroup) storagebudget.CapacityVector {
	return storagebudget.CapacityVector{Bytes: ((g.Bytes+65535)/65536)*131072 + (g.Files+g.Directories+g.MetadataOps+4)*65536, Inodes: g.Files + g.Directories + 2}
}
func epScope(path byte, captured bool, record storageOwnerRecordV2, before, final [2]uint64) storageEnrollmentPeakScope {
	var s storageEnrollmentPeakScope
	epSet16(s[:], 1)
	s[2] = 1
	s[3] = path
	epSet32(s[4:], 7)
	s[8] = 8
	s[24] = 9
	copy(s[40:56], record.Installation[:])
	copy(s[56:88], record.QBinding[:])
	for _, i := range []int{88, 120, 152, 408} {
		s[i] = 1
	}
	epAtNative(s[216:251], record.Config)
	epAtNative(s[251:286], epNative(12, 1))
	epAtNative(s[286:321], epNative(100, 1))
	if captured {
		s[321] = 15
	}
	s[322] = 1
	s[323] = 255
	s[324] = 255
	s[325] = 0
	s[326] = 1
	if path != 1 {
		s[323] = 0
		s[324] = 0
	}
	for i := 0; i < 2; i++ {
		epSet64(s[328+i*8:], before[i])
		epSet64(s[344+i*8:], final[i])
	}
	growth := final[0] - before[0] + final[1] - before[1]
	if path == 1 {
		growth += 8192
		epSet64(s[368:], 4)
	}
	epSet64(s[360:], growth)
	epSet64(s[400:], 134217728)
	epSet64(s[1616:], 1)
	epSet64(s[1624:], 1)
	epSet16(s[1634:], 2)
	epSet32(s[1636:], 31)
	for i := 0; i < 8; i++ {
		p := epNative(uint64(200+i), 2)
		switch i {
		case 4:
			p = record.CatalogA
		case 5:
			p = record.CatalogB
		case 6:
			p = epNative(22, 2)
		case 7:
			p = epNative(23, 2)
		}
		if i >= 4 || captured {
			epAtNative(s[1080+35*i:], p)
		}
	}
	groups := epGroups(path, final, s[322], s[323], s[324], s[325])
	epSet16(s[1632:], uint16(len(groups)))
	var parent [2]storagebudget.CapacityVector
	var target [10]uint64
	for _, g := range groups {
		v := epEstimate(g)
		parent[g.Parent-1].Bytes += v.Bytes
		parent[g.Parent-1].Inodes += v.Inodes
		target[g.Target-1] += v.Bytes
	}
	epSet64(s[384:], parent[0].Bytes+parent[1].Bytes)
	epSet64(s[392:], parent[0].Inodes+parent[1].Inodes)
	s[440] = 10
	epSet64(s[456:], 1)
	s[464] = 3
	s[488] = 1
	s[504] = 1
	epSet64(s[472:], parent[0].Bytes+parent[1].Bytes)
	epSet64(s[480:], parent[0].Inodes+parent[1].Inodes)
	for i := 0; i < 8; i++ {
		d := s[1360+i*32 : 1360+(i+1)*32]
		d[0] = 1
		if i >= 6 {
			d[0] = 2
		}
		d[1] = byte(i + 1)
		if path == 1 && i < 4 {
			d[2] = 1
		}
		bef, fin := uint64(0), uint64(0)
		switch {
		case i < 2:
			bef = before[i]
			fin = final[i]
		case i < 4:
			fin = 4096
			if path != 1 {
				bef = 4096
			}
		case i < 6:
			bef = 8388608
			fin = bef
		default:
			bef = 4194304
			fin = bef
		}
		epSet64(d[4:], bef)
		epSet64(d[12:], fin)
		epSet64(d[20:], target[i])
	}
	epScopeHash(&s)
	return s
}

func TestStorageEnrollmentPeakScopeVectors(t *testing.T) {
	_, r, _ := ownerAdvisoryFixture(false)
	for _, v := range []struct {
		path          byte
		before, final [2]uint64
		groups        int
		peak          uint64
	}{{1, [2]uint64{}, [2]uint64{71303168, 71303168}, 62, 814350336}, {2, [2]uint64{71303168, 71303168}, [2]uint64{71303168, 71303168}, 44, 488374272}, {3, [2]uint64{65536, 65536}, [2]uint64{71303168, 71303168}, 54, 820248576}} {
		t.Run(fmt.Sprint(v.path), func(t *testing.T) {
			s := epScope(v.path, true, r, v.before, v.final)
			got, err := storageEnrollmentPeakDecodeScope(s[:])
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := storageEnrollmentPeakEncodeScope(s)
			if err != nil || !bytes.Equal(encoded, s[:]) {
				t.Fatal("exact Scope bytes", err)
			}
			if got != s || len(storageEnrollmentPeakPlan(s)) != v.groups || binary.LittleEndian.Uint64(s[384:]) != v.peak {
				t.Fatal("handwritten finite plan / whole peak")
			}
			want := epGroups(v.path, v.final, s[322], s[323], s[324], s[325])
			for i, g := range storageEnrollmentPeakPlan(s) {
				w := want[i]
				w.Generation = 0
				w.Native = s.target(w.Target)
				if g != w {
					t.Fatalf("row %d differs: %#v %#v", i, g, w)
				}
			}
			for _, offset := range []int{0, 2, 3, 4, 8, 24, 40, 56, 88, 120, 152, 184, 216, 251, 286, 321, 322, 323, 324, 325, 326, 327, 344, 360, 384, 400, 408, 440, 456, 464, 465, 472, 480, 488, 504, 520, 1080, 1360, 1361, 1362, 1363, 1364, 1372, 1380, 1388, 1616, 1624, 1632, 1634, 1636, 1640, 4095} {
				bad := s
				bad[offset] ^= 128
				if _, err := storageEnrollmentPeakDecodeScope(bad[:]); err == nil {
					t.Fatalf("accepted mutation at %d", offset)
				}
			}
			if _, err := storageEnrollmentPeakDecodeScope(s[:4095]); err == nil {
				t.Fatal("truncated")
			}
		})
	}
	s := epScope(1, false, r, [2]uint64{}, [2]uint64{65536, 65536})
	after := s
	after[321] = 15
	for i := 0; i < 4; i++ {
		epAtNative(after[1080+i*35:], epNative(uint64(200+i), 2))
	}
	if s.policyDigest() != after.policyDigest() || !storageEnrollmentPeakCaptureMatches(s, after) {
		t.Fatal("only prospective capture fields")
	}
	after[408]++
	if storageEnrollmentPeakCaptureMatches(s, after) {
		t.Fatal("capture changed qualified-source binding")
	}
	for _, g := range []storageEnrollmentPeakGroup{{Bytes: math.MaxUint64}, {Files: math.MaxUint64}, {MetadataOps: math.MaxUint64}} {
		if _, ok := storageEnrollmentPeakEstimate(g); ok {
			t.Fatal("estimate overflow")
		}
	}
}

func TestStorageEnrollmentPeakPackCanonicalVectors(t *testing.T) {
	c, _, _ := ownerAdvisoryFixture(false)
	raw := epJSON(c)
	packed, err := storageEnrollmentPeakPackV3(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := storageEnrollmentPeakUnpackV3(packed, uint32(len(raw)))
	if err != nil || !bytes.Equal(out, raw) {
		t.Fatal("lossless", err)
	}
	if packed[0] != 3 {
		t.Fatal("Version must be first shortest ULEB")
	}
	for name, bad := range map[string][]byte{"unknown": append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"Extra":0}`)...), "missing": bytes.Replace(raw, []byte(`"Version":3,`), nil, 1), "order": bytes.Replace(raw, []byte(`{"Version":3,"Installation":`), []byte(`{"Installation":`), 1), "space": append([]byte(" "), raw...), "newline": append(append([]byte{}, raw...), 10), "null-list": bytes.Replace(raw, []byte(`"Entries":[]`), []byte(`"Entries":null`), 1), "duplicate": bytes.Replace(raw, []byte(`"Version":3`), []byte(`"Version":3,"Version":3`), 1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := storageEnrollmentPeakPackV3(bad); err == nil {
				t.Fatal("accepted noncanonical tree")
			}
		})
	}
	for _, v := range []struct {
		raw  []byte
		bits int
		want uint64
		ok   bool
	}{{[]byte{0}, 64, 0, true}, {[]byte{127}, 8, 127, true}, {[]byte{128, 1}, 8, 128, true}, {[]byte{255, 1}, 8, 255, true}, {[]byte{128, 2}, 8, 0, false}, {[]byte{128, 0}, 64, 0, false}, {[]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 1}, 64, math.MaxUint64, true}, {[]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 2}, 64, 0, false}, {[]byte{128}, 64, 0, false}} {
		u := storageEnrollmentPeakUnpacker{raw: v.raw}
		n, ok := u.uint(v.bits)
		if ok != v.ok || ok && n != v.want {
			t.Fatal("literal ULEB", v, n, ok)
		}
	}
	for _, bad := range [][]byte{nil, packed[:len(packed)-1], append(append([]byte{}, packed...), 0), append([]byte{131, 0}, packed[1:]...), append([]byte{1}, packed[1:]...)} {
		if _, err := storageEnrollmentPeakUnpackV3(bad, uint32(len(raw))); err == nil {
			t.Fatal("malformed packed root")
		}
	}
	if _, err := storageEnrollmentPeakUnpackV3(packed, uint32(len(raw)+1)); err == nil {
		t.Fatal("wrong original length")
	}
	c.Version = 1
	inner := epJSON(c)
	ip, err := storageEnrollmentPeakPackInner(inner)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := storageEnrollmentPeakUnpackInner(ip)
	if err != nil || !bytes.Equal(restored, inner) {
		t.Fatal("inner complete closed tree", err)
	}
	if _, err := storageEnrollmentPeakPackV3(inner); err == nil {
		t.Fatal("version crossing")
	}
}

// Fill an original-valid v3 catalog to its exact 8MiB-64 payload bound. Relative
// paths are authored valid components, and every source occurrence still counts
// against 4MiB. Only independent JSON/domain/old material helpers seal the data.
func epV3Size(t *testing.T, target int) (storageOwnerCatalogV2, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	c, r, cl := ownerAdvisoryFixture(false)
	base := len(c.Roots)
	for i := 0; i < 1900; i++ {
		index := uint32(len(c.Objects))
		c.Objects = append(c.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: math.MaxUint32, Name: []byte{}, Persistent: epNative(uint64(100000+i), 1), OwnerClass: 2, ModeClass: 1})
		root := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{128, byte(i >> 8), byte(i)}, Filesystem: c.Filesystems[0].Definition.ID, Generation: 1, Locator: fmt.Sprintf("external:%d", i), CapacityCount: c.Roots[0].Definition.CapacityCount, CapacityRefs: c.Roots[0].Definition.CapacityRefs}, Role: 6, Category: 4, References: []storagebudget.ID{}, Source: storageOwnerRootSource{Kind: 6, OwnerKey: []byte(fmt.Sprintf("original-family:%d", i)), Path: []byte(fmt.Sprintf("/external/%d", i)), Relative: []byte{}, ControlIndex: math.MaxUint32}, DirectoryObject: index, DomainObject: index, WorkObject: math.MaxUint32, Membership: c.Roots[0].Membership}
		root.Membership.ExistingObjects = []uint32{}
		c.Roots = append(c.Roots, root)
	}
	c.Sequence = 2
	c.PreviousCatalogDigest = storagebudget.Digest{1}
	c.Transition = storagebudget.ID{1}
	ownerCatalogSeal(&c, &r, &cl)
	// Reserve >131 bytes for the independent digest-length variation and neutral
	// transition scalars. A component chain is <=65599 and has no empty segment.
	for i := base; i < len(c.Roots); i++ {
		remaining := target - len(epJSON(c)) - 16384
		if remaining <= 0 {
			break
		}
		n := remaining * 3 / 4
		if n > 65000 {
			n = 65000
		}
		if n < 1 {
			break
		}
		parts := []string{}
		remainingBytes := n
		for remainingBytes > 0 {
			component := remainingBytes
			if component > 1024 {
				component = 1024
			}
			if remainingBytes == 1025 {
				component = 1023
			}
			parts = append(parts, strings.Repeat("a", component))
			remainingBytes -= component
			if remainingBytes > 0 {
				remainingBytes--
			}
		}
		c.Roots[i].Source.Relative = []byte(strings.Join(parts, "/"))
	}
	ownerCatalogSeal(&c, &r, &cl)
	gap := target - len(epJSON(c))
	if gap < 150 {
		t.Fatal("fixture reserve", gap)
	}
	padding := gap - 65
	for i := base; padding > 0 && i < len(c.Roots); i++ {
		n := 256 - len(c.Roots[i].Definition.Locator)
		if n > padding {
			n = padding
		}
		c.Roots[i].Definition.Locator += strings.Repeat("x", n)
		padding -= n
	}
	if padding != 0 {
		t.Fatal("locator capacity")
	}
	ownerCatalogSeal(&c, &r, &cl)
	gap = target - len(epJSON(c))
	if gap < 0 || gap > 134 {
		t.Fatal("digest-length reserve", gap)
	}
	add := func(v *byte) {
		for *v < 100 && gap > 0 {
			if *v < 10 {
				*v = 10
				gap--
			} else {
				*v = 100
				gap--
			}
		}
	}
	for i := range c.Transition {
		add(&c.Transition[i])
	}
	for i := range c.PreviousCatalogDigest {
		add(&c.PreviousCatalogDigest[i])
	}
	for gap > 0 && c.ExpectedQRevision <= 1000000000000000000 {
		c.ExpectedQRevision *= 10
		gap--
	}
	for gap > 0 && c.Sequence <= 1000000000000000000 {
		c.Sequence *= 10
		gap--
	}
	if gap != 0 || len(epJSON(c)) != target {
		t.Fatal("exact bound construction", gap, len(epJSON(c)))
	}
	return c, r, cl
}
func TestStorageEnrollmentPeakPackMaximumOldV3(t *testing.T) {
	c, r, cl := epMaxV3(t)
	payload := epJSON(c)
	raw := ownerAdvisoryWire(c)
	if _, err := storageOwnerDecodeCatalogV3(raw, r, cl); err != nil {
		t.Fatal("max fixture must be original-valid", err)
	}
	packed, err := storageEnrollmentPeakPackV3(payload)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := storageEnrollmentPeakUnpackV3(packed, 8388544)
	if err != nil || !bytes.Equal(payload, restored) {
		t.Fatal("exact max lossless", err)
	}
	if len(payload) != 8388544 || 4480+len(packed) > 8388608 {
		t.Fatal("maximum encoded room", len(payload), len(packed))
	}
	s := epScope(1, false, r, [2]uint64{}, [2]uint64{65536, 65536})
	epSet64(s[1624:], c.Sequence)
	fullDigest := sha256.Sum256(raw)
	copy(s[88:120], fullDigest[:])
	epScopeHash(&s)
	snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: fullDigest, Inner: epHash("desk-enrollment-original-v3-inner-peak-v1", payload), Before: raw}
	fence, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: c.Sequence + 1, Scope: s, Snapshot: &snapshot}, r, cl)
	if err != nil || fence[66] != 1 {
		t.Fatal("max full original uses lossless PackV3 snapshot", err)
	}
	decoded, err := storageEnrollmentPeakDecodeEnvelope(fence, r, cl, true)
	if err != nil || decoded.Snapshot == nil || !bytes.Equal(decoded.Snapshot.Before, raw) {
		t.Fatal("max exact original full state", err)
	}
	t.Logf("original payload=%d PackV3=%d Fence=%d spare=%d", len(payload), len(packed), 4480+len(packed), 8388608-4480-len(packed))
}

func TestStorageEnrollmentPeakLiteralPrimitiveBytes(t *testing.T) {
	type closed struct {
		V        uint64
		Truth    bool
		Fixed    [2]uint8
		Relative []byte
		Phase    string
		Roots    []uint16
	}
	value := closed{128, true, [2]uint8{1, 255}, []byte{0, 255}, "<>&", []uint16{1, 128}}
	want := []byte{0x80, 1, 1, 1, 0xff, 1, 2, 0, 0xff, 3, 0x3c, 0x3e, 0x26, 2, 1, 0x80, 1}
	var packed []byte
	if !storageEnrollmentPeakPackValue(&packed, reflect.ValueOf(value)) || !bytes.Equal(packed, want) {
		t.Fatal("literal pinned primitive bytes", packed)
	}
	const jsonBytes = `{"V":128,"Truth":true,"Fixed":[1,255],"Relative":"AP8=","Phase":"\u003c\u003e\u0026","Roots":[1,128]}`
	p := storageEnrollmentPeakUnpacker{raw: want, limit: len(jsonBytes)}
	if !p.value(reflect.TypeOf(value), "") || p.pos != len(want) || string(p.out) != jsonBytes {
		t.Fatal("literal canonical JSON", string(p.out))
	}
	for _, bad := range []struct {
		at    int
		value byte
	}{{2, 2}, {3, 0x80}, {9, 0xff}, {13, 0xff}} {
		raw := append([]byte{}, want...)
		raw[bad.at] = bad.value
		p := storageEnrollmentPeakUnpacker{raw: raw, limit: len(jsonBytes)}
		if p.value(reflect.TypeOf(value), "") && p.pos == len(raw) {
			t.Fatal("primitive malformed accepted", bad)
		}
	}
	value.Relative = nil
	packed = nil
	if storageEnrollmentPeakPackValue(&packed, reflect.ValueOf(value)) {
		t.Fatal("nil field repaired")
	}
}

func epMaxV3(t *testing.T) (storageOwnerCatalogV2, storageOwnerRecordV2, storageOwnerClaimV2) {
	return epV3Size(t, 8388544)
}

// These three objects are handwritten inputs. Neither native issuance nor a
// successful application route is inferred from accepting their catalog bytes.
func storageTransferControlObjectVectors() []storageOwnerObject {
	return []storageOwnerObject{
		{Role: 25, Source: 1, Parent: 0, Name: []byte(".data-transfer.lock"), Persistent: epNative(200, 2), OwnerClass: 1, ModeClass: 2},
		{Role: 26, Source: 1, Parent: 0, Name: []byte("storage-transfer.a"), Persistent: epNative(201, 2), OwnerClass: 1, ModeClass: 2, FixedLength: 1384448},
		{Role: 27, Source: 1, Parent: 0, Name: []byte("storage-transfer.b"), Persistent: epNative(202, 2), OwnerClass: 1, ModeClass: 2, FixedLength: 1384448},
	}
}

func TestStorageTransferControlsLogicalPAndLegacyV2(t *testing.T) {
	original, record, claim := ownerAdvisoryFixture(false)
	original.Objects = append(original.Objects, storageTransferControlObjectVectors()...)
	if _, err := storageOwnerDecodeCatalogV3(ownerAdvisoryWire(original), record, claim); err != nil {
		t.Fatal("original V3 transfer controls refused", err)
	}
	logical := ownerCatalogClone(original)
	logical.Version = 1
	if err := storageEnrollmentPeakValidateCatalog(logical, record, claim); err != nil {
		t.Fatal("logical P transfer controls refused", err)
	}
	controls := [4]storageOwnerPersistentObject{epNative(210, 2), epNative(211, 2), epNative(212, 2), epNative(213, 2)}
	raw, err := storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: logical, Controls: controls}, record, claim)
	if err != nil {
		t.Fatal("logical P inner encode", err)
	}
	decoded, err := storageEnrollmentPeakDecodeInner(raw, record, claim)
	if err != nil || decoded.Catalog.Version != 1 || !reflect.DeepEqual(decoded.Catalog, logical) || decoded.Controls != controls || original.Version != 3 {
		t.Fatal("logical P family or original V3 changed", err)
	}
	packed, err := storageEnrollmentPeakPackInner(epJSON(logical))
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := storageEnrollmentPeakUnpackInner(packed)
	if err != nil || !bytes.Equal(unpacked, epJSON(logical)) {
		t.Fatal("logical P packed tree changed", err)
	}
	// Use an actual otherwise-valid V2 payload and V2 envelope. Advisory V3
	// meanings or a V3 magic/version cannot be the reason this decoder refuses.
	legacy, oldRecord, oldClaim := ownerCatalogFixture()
	if _, err := storageOwnerDecodeCatalogV2(ownerCatalogWire(ownerCatalogJSON(legacy), legacy.Sequence), oldRecord, oldClaim); err != nil {
		t.Fatal("legacy baseline", err)
	}
	legacy.Objects = append(legacy.Objects, storageTransferControlObjectVectors()...)
	if _, err := storageOwnerDecodeCatalogV2(ownerCatalogWire(ownerCatalogJSON(legacy), legacy.Sequence), oldRecord, oldClaim); err == nil {
		t.Fatal("actual V2 reader accepted transfer control roles")
	}
	for mask := 1; mask < 7; mask++ {
		bad := ownerCatalogClone(logical)
		bad.Objects = bad.Objects[:len(bad.Objects)-3]
		for i, object := range storageTransferControlObjectVectors() {
			if mask&(1<<i) != 0 {
				bad.Objects = append(bad.Objects, object)
			}
		}
		if storageEnrollmentPeakValidateCatalog(bad, record, claim) == nil {
			t.Fatal("partial transfer control set accepted", mask)
		}
	}
	for _, cut := range []string{"duplicate-role", "duplicate-native", "wrong-native-kind", "wrong-parent", "wrong-size", "wrong-source", "wrong-name"} {
		bad := ownerCatalogClone(logical)
		last := len(bad.Objects) - 1
		switch cut {
		case "duplicate-role":
			bad.Objects[last].Role = 26
			bad.Objects[last].Name = []byte("storage-transfer.a")
		case "duplicate-native":
			bad.Objects[last].Persistent = bad.Objects[last-1].Persistent
		case "wrong-native-kind":
			bad.Objects[last].Persistent.ObjectKind = 1
		case "wrong-parent":
			bad.Objects[last].Parent = 1
		case "wrong-size":
			bad.Objects[last].FixedLength--
		case "wrong-source":
			bad.Objects[last].Source = 3
			bad.Objects[last].OwnerClass = 2
		case "wrong-name":
			bad.Objects[last].Name = []byte("foreign-transfer.b")
		}
		if storageEnrollmentPeakValidateCatalog(bad, record, claim) == nil {
			t.Fatal("malformed logical P transfer object accepted", cut)
		}
		if _, err := storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: bad, Controls: controls}, record, claim); err == nil {
			t.Fatal("inner encoder accepted malformed transfer object", cut)
		}
	}
}
