package desk

import (
	"bytes"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageEnrollmentPeakOrigin struct {
	ID, Bridge       storagebudget.ID
	Epoch, Revision  uint64
	View, Projection uint16
	Slots            uint64
	Witnesses        []uint16
	Digest           storagebudget.Digest
}
type storageEnrollmentPeakCoverage struct {
	Slot, Origin, Witness uint16
	Generation            uint64
	Operation             storagebudget.ID
	Page                  storagebudget.Digest
	Epoch                 uint64
	Bridge                storagebudget.ID
	State                 uint8
}
type storageEnrollmentPeakEndpoint struct {
	Bridge            storagebudget.ID
	Epoch, Q3Revision uint64
	Catalog, Evidence storagebudget.Digest
}
type storageEnrollmentPeakBoard struct {
	Groups       []storageEnrollmentPeakGroup
	Origins      []storageEnrollmentPeakOrigin
	Coverage     []storageEnrollmentPeakCoverage
	Prior        storageEnrollmentPeakEndpoint
	Registration [4][]uint16
	SourceKind   uint8
	Source       storageEnrollmentPeakSource
	Raw          []byte
}
type storageEnrollmentPeakBoardDigests struct {
	Q      [3]storagebudget.Digest
	Origin storagebudget.Digest
}

func storageEnrollmentPeakWriteGroup(g storageEnrollmentPeakGroup) []byte {
	b := make([]byte, 115)
	storageEnrollmentPeakPut16(b[:2], g.ID)
	b[2] = g.Parent
	b[3] = g.Target
	storageEnrollmentPeakPut64(b[4:12], g.Bytes)
	storageEnrollmentPeakPut64(b[12:20], g.Files)
	storageEnrollmentPeakPut64(b[20:28], g.Directories)
	storageEnrollmentPeakPut64(b[28:36], g.MetadataOps)
	storageEnrollmentPeakPut16(b[36:38], g.Completion)
	b[38] = g.State
	copy(b[40:75], storageEnrollmentPeakNativeBytes(g.Native))
	copy(b[75:107], g.Expected[:])
	storageEnrollmentPeakPut64(b[107:115], g.Generation)
	return b
}
func storageEnrollmentPeakReadGroup(raw []byte) (storageEnrollmentPeakGroup, error) {
	if len(raw) != 115 || raw[39] != 0 || raw[38] > 3 {
		return storageEnrollmentPeakGroup{}, storagebudget.ErrBounds
	}
	g := storageEnrollmentPeakGroup{ID: storageEnrollmentPeakU16(raw[:2]), Parent: raw[2], Target: raw[3], Bytes: storageEnrollmentPeakU64(raw[4:12]), Files: storageEnrollmentPeakU64(raw[12:20]), Directories: storageEnrollmentPeakU64(raw[20:28]), MetadataOps: storageEnrollmentPeakU64(raw[28:36]), Completion: storageEnrollmentPeakU16(raw[36:38]), State: raw[38], Native: storageEnrollmentPeakNative(raw[40:75]), Generation: storageEnrollmentPeakU64(raw[107:115])}
	copy(g.Expected[:], raw[75:107])
	if g.Generation == 0 {
		return storageEnrollmentPeakGroup{}, storagebudget.ErrBounds
	}
	return g, nil
}

func storageEnrollmentPeakReadBoard(raw []byte, s storageEnrollmentPeakScope, phase byte, digests storageEnrollmentPeakBoardDigests, c storageOwnerCatalogV2, views, projections, witnesses int) (storageEnrollmentPeakBoard, error) {
	var b storageEnrollmentPeakBoard
	if len(raw) > 65536 || s[321] != 15 || phase > 2 {
		return b, storagebudget.ErrBounds
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if r.u16() != 2 || !bytes.Equal(r.take(16), s[24:40]) || r.native() != s.target(9) || r.native() != s.target(10) || !bytes.Equal(r.take(32), s[184:216]) {
		return b, storagebudget.ErrBounds
	}
	n := int(r.u8())
	if n != int(s[326]) {
		return b, storagebudget.ErrBounds
	}
	for i := 0; i < n; i++ {
		if !bytes.Equal(r.take(24), s[440+i*80:464+i*80]) {
			return b, storagebudget.ErrBounds
		}
	}
	if !bytes.Equal(r.take(24), s[360:384]) {
		return b, storagebudget.ErrBounds
	}
	writeSum := r.u64()
	if !bytes.Equal(r.take(16), s[384:400]) || !bytes.Equal(r.take(4), s[322:326]) {
		return b, storagebudget.ErrBounds
	}
	n = int(r.u8())
	plan := storageEnrollmentPeakPlan(s)
	if n != len(plan) || n > 62 {
		return b, storagebudget.ErrBounds
	}
	b.Groups = make([]storageEnrollmentPeakGroup, n)
	sum := uint64(0)
	for i, want := range plan {
		g, err := storageEnrollmentPeakReadGroup(r.take(115))
		if err != nil {
			return storageEnrollmentPeakBoard{}, err
		}
		expected := storagebudget.Digest{}
		id := g.ID
		if id >= 16 && id <= 20 || id >= 51 && id <= 55 {
			expected = digests.Origin
			if expected == (storagebudget.Digest{}) {
				return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
			}
		}
		qIndex := -1
		switch id {
		case 21, 56:
			qIndex = 0
		case 22, 57:
			qIndex = 1
		case 23, 58:
			qIndex = 2
		}
		if qIndex >= 0 {
			if qIndex == 2 && phase == 0 {
				expected = storagebudget.Digest{}
			} else {
				expected = digests.Q[qIndex]
				if expected == (storagebudget.Digest{}) {
					return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
				}
			}
		}
		if g.ID != want.ID || g.Parent != want.Parent || g.Target != want.Target || g.Bytes != want.Bytes || g.Files != want.Files || g.Directories != want.Directories || g.MetadataOps != want.MetadataOps || g.Completion != want.Completion || g.Native != want.Native || g.Expected != expected {
			return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
		}
		sum += g.Bytes
		b.Groups[i] = g
	}
	if writeSum != sum {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	n = int(r.u16())
	if n > 64 {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	b.Origins = make([]storageEnrollmentPeakOrigin, n)
	slots := uint64(0)
	witnessCount := 0
	for i := range b.Origins {
		start := r.pos
		o := storageEnrollmentPeakOrigin{ID: r.id(), Bridge: r.id(), Epoch: r.u64(), Revision: r.u64(), View: r.u16(), Projection: r.u16(), Slots: r.u64()}
		count := int(r.u16())
		witnessCount += count
		if count > 128 || witnessCount > 128 || o.ID == (storagebudget.ID{}) || o.Bridge == (storagebudget.ID{}) || o.Epoch == 0 || o.Revision == 0 || int(o.View) >= views || int(o.Projection) >= projections || o.Slots == 0 || slots&o.Slots != 0 || i > 0 && bytes.Compare(b.Origins[i-1].ID[:], o.ID[:]) >= 0 {
			return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
		}
		slots |= o.Slots
		o.Witnesses = make([]uint16, count)
		for j := range o.Witnesses {
			o.Witnesses[j] = r.u16()
			if int(o.Witnesses[j]) >= witnesses || j > 0 && o.Witnesses[j-1] >= o.Witnesses[j] {
				return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
			}
		}
		end := r.pos
		o.Digest = r.digest()
		if r.bad || storageEnrollmentPeakHash("desk-enrollment-origin-record-e1", raw[start:end]) != o.Digest {
			return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
		}
		b.Origins[i] = o
	}
	n = int(r.u16())
	if n > 64 {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	b.Coverage = make([]storageEnrollmentPeakCoverage, n)
	covered := uint64(0)
	for i := range b.Coverage {
		v := storageEnrollmentPeakCoverage{Slot: r.u16(), Origin: r.u16(), Witness: r.u16(), Generation: r.u64(), Operation: r.id(), Page: r.digest(), Epoch: r.u64(), Bridge: r.id(), State: r.u8()}
		if v.Slot >= 64 || int(v.Origin) >= len(b.Origins) || int(v.Witness) >= witnesses || v.Generation == 0 || v.Operation == (storagebudget.ID{}) || v.Page == (storagebudget.Digest{}) || v.Epoch == 0 || v.Bridge == (storagebudget.ID{}) || v.State < 1 || v.State > 2 || i > 0 && b.Coverage[i-1].Slot >= v.Slot || b.Origins[v.Origin].Slots&(uint64(1)<<v.Slot) == 0 {
			return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
		}
		covered |= uint64(1) << v.Slot
		b.Coverage[i] = v
	}
	if covered != slots {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	b.Prior = storageEnrollmentPeakEndpoint{Bridge: r.id(), Epoch: r.u64(), Q3Revision: r.u64(), Catalog: r.digest(), Evidence: r.digest()}
	if s.path() == 1 {
		if b.Prior != (storageEnrollmentPeakEndpoint{}) {
			return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
		}
	} else if b.Prior.Bridge == (storagebudget.ID{}) || b.Prior.Epoch == 0 || b.Prior.Q3Revision == 0 || !bytes.Equal(b.Prior.Catalog[:], s[88:120]) || b.Prior.Evidence == (storagebudget.Digest{}) {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	limits := [4]int{1, 0, 1, 4}
	for k := range b.Registration {
		n := int(r.u16())
		if n > limits[k] {
			return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
		}
		b.Registration[k] = make([]uint16, n)
		for j := range b.Registration[k] {
			b.Registration[k][j] = r.u16()
			if j > 0 && b.Registration[k][j-1] >= b.Registration[k][j] {
				return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
			}
		}
	}
	b.SourceKind = r.u8()
	if b.SourceKind != 7 && b.SourceKind != 8 && (!s.hasOriginalPeer() || b.SourceKind != 9 && b.SourceKind != 10 && b.SourceKind != 11) {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	source, err := storageEnrollmentPeakScanSource(&r)
	if err != nil || !r.zeroTail() {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	b.Source = source
	b.Raw = append([]byte{}, raw...)
	if !storageEnrollmentPeakRegistrationJoins(b, c) {
		return storageEnrollmentPeakBoard{}, storagebudget.ErrBounds
	}
	return b, nil
}

func storageEnrollmentPeakRegistrationJoins(b storageEnrollmentPeakBoard, c storageOwnerCatalogV2) bool {
	before, _, err := c.definitions()
	if err != nil {
		return false
	}
	lists := [][]storagebudget.ID{{}, {}, {}, {}}
	for _, v := range before.Roots {
		lists[0] = append(lists[0], v.ID)
	}
	for _, v := range before.Workspaces {
		lists[1] = append(lists[1], v.ID)
	}
	for _, v := range before.Filesystems {
		lists[2] = append(lists[2], v.ID)
	}
	for _, v := range before.Constraints {
		lists[3] = append(lists[3], v.ID)
	}
	dict := []reflect.Value{reflect.ValueOf(c.Roots), reflect.ValueOf(c.Workspaces), reflect.ValueOf(c.Filesystems), reflect.ValueOf(c.Constraints)}
	for k, v := range dict {
		want := []uint16{}
		for i := 0; i < v.Len(); i++ {
			id := v.Index(i).FieldByName("Definition").FieldByName("ID").Interface().(storagebudget.ID)
			found := false
			for _, old := range lists[k] {
				if id == old {
					found = true
					break
				}
			}
			if !found {
				want = append(want, uint16(i))
			}
		}
		if !reflect.DeepEqual(want, b.Registration[k]) {
			return false
		}
	}
	if b.SourceKind == 9 {
		if len(b.Registration[0]) > 1 || !storageEnrollmentDefaultHistorySourceJoins(b.Source, c) {
			return false
		}
		if len(b.Registration[0]) == 1 {
			r := c.Roots[b.Registration[0][0]]
			return c.Objects[r.DirectoryObject].Persistent == b.Source.Default.Selected
		}
		return len(b.Registration[2]) == 0 && len(b.Registration[3]) == 0
	}
	if b.SourceKind == 10 && b.Source.Default != nil && b.Source.Default.Workflow == 4 && len(b.Registration[0]) == 0 {
		return storageTransferRetentionRegistration(b, c)
	}
	if b.SourceKind == 11 && len(b.Registration[0]) == 0 {
		_, ok := storageTransferOwningCompletionRegistration(b, c)
		return ok
	}
	if len(b.Registration[0]) != 1 || int(b.Registration[0][0]) >= len(c.Roots) {
		return false
	}
	root := c.Roots[b.Registration[0][0]]
	sourceRaw, err := storageEnrollmentPeakEncodeSource(b.Source)
	if err != nil {
		return false
	}
	if b.SourceKind == 11 {
		return root.Source.Kind == 11 && b.Source.Default != nil && b.Source.Default.Workflow == 6 && storageTransferCapturedRootJoins(c, root) == nil && bytes.Equal(root.Source.Relative, sourceRaw)
	}
	if b.SourceKind == 10 {
		return root.Role == 8 && root.Source.Kind == 9 && b.Source.Default != nil && (b.Source.Default.Workflow == 2 || b.Source.Default.Workflow == 4) && bytes.Equal(root.Source.Relative, sourceRaw)
	}
	if b.SourceKind == 7 {
		return b.Source.Default == nil && b.Source.ActionPageDigest == (storagebudget.Digest{}) && root.Role == 7 && root.Source.Kind == 7 && bytes.Equal(root.Source.Relative, sourceRaw)
	}
	if b.SourceKind != 8 || b.Source.Default != nil || len(b.Registration[2]) != 0 || len(b.Registration[3]) != 0 || root.Source.Kind != 8 {
		return false
	}
	parentSource, err := storageEnrollmentPrivateParentSource(b.Source)
	if err != nil {
		return false
	}
	for _, p := range c.Roots {
		if p.Definition.ID == root.Source.ParentRoot && p.Role == 7 && bytes.Equal(p.Source.Relative, parentSource) {
			return true
		}
	}
	return false
}

// This writer preserves all diagnostic states and fixed operands, then uses
// the same strict reader. It has no state-transition or effect method.
func storageEnrollmentPeakWriteBoard(b storageEnrollmentPeakBoard, s storageEnrollmentPeakScope, phase byte, digests storageEnrollmentPeakBoardDigests, c storageOwnerCatalogV2, views, projections, witnesses int) ([]byte, error) {
	w := storageEnrollmentPeakWriter{max: 65536}
	w.u16(2)
	w.put(s[24:40])
	w.put(s[216:251])
	w.put(s[251:286])
	w.put(s[184:216])
	w.u8(s[326])
	for i := 0; i < int(s[326]); i++ {
		w.put(s[440+i*80 : 464+i*80])
	}
	w.put(s[360:384])
	sum := uint64(0)
	for _, g := range b.Groups {
		sum += g.Bytes
	}
	w.u64(sum)
	w.put(s[384:400])
	w.put(s[322:326])
	if len(b.Groups) > 62 {
		return nil, storagebudget.ErrBounds
	}
	w.u8(byte(len(b.Groups)))
	for _, g := range b.Groups {
		w.put(storageEnrollmentPeakWriteGroup(g))
	}
	if len(b.Origins) > 64 || len(b.Coverage) > 64 {
		return nil, storagebudget.ErrBounds
	}
	w.u16(uint16(len(b.Origins)))
	for _, o := range b.Origins {
		start := len(w.raw)
		w.put(o.ID[:])
		w.put(o.Bridge[:])
		w.u64(o.Epoch)
		w.u64(o.Revision)
		w.u16(o.View)
		w.u16(o.Projection)
		w.u64(o.Slots)
		if len(o.Witnesses) > 128 {
			return nil, storagebudget.ErrBounds
		}
		w.u16(uint16(len(o.Witnesses)))
		for _, v := range o.Witnesses {
			w.u16(v)
		}
		if w.bad {
			return nil, storagebudget.ErrBounds
		}
		computed := storageEnrollmentPeakHash("desk-enrollment-origin-record-e1", w.raw[start:])
		if computed != o.Digest {
			return nil, storagebudget.ErrBounds
		}
		w.put(o.Digest[:])
	}
	w.u16(uint16(len(b.Coverage)))
	for _, v := range b.Coverage {
		w.u16(v.Slot)
		w.u16(v.Origin)
		w.u16(v.Witness)
		w.u64(v.Generation)
		w.put(v.Operation[:])
		w.put(v.Page[:])
		w.u64(v.Epoch)
		w.put(v.Bridge[:])
		w.u8(v.State)
	}
	w.put(b.Prior.Bridge[:])
	w.u64(b.Prior.Epoch)
	w.u64(b.Prior.Q3Revision)
	w.put(b.Prior.Catalog[:])
	w.put(b.Prior.Evidence[:])
	for _, list := range b.Registration {
		if len(list) > 4 {
			return nil, storagebudget.ErrBounds
		}
		w.u16(uint16(len(list)))
		for _, v := range list {
			w.u16(v)
		}
	}
	w.u8(b.SourceKind)
	source, err := storageEnrollmentPeakEncodeSource(b.Source)
	if err != nil {
		return nil, err
	}
	w.put(source)
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	raw := make([]byte, 65536)
	copy(raw, w.raw)
	if _, err := storageEnrollmentPeakReadBoard(raw, s, phase, digests, c, views, projections, witnesses); err != nil {
		return nil, err
	}
	return raw, nil
}
