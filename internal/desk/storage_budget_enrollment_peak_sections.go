package desk

import (
	"bytes"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Effective sets are comparison operands. Complete ID/generation/value bytes,
// not a digest or the current default, identify a historical set. They own all
// slices and cannot supply native, Q, registration or effect authority.
type storagePolicyEffectiveSet struct {
	Policy     storageOwnerPolicy
	Workspaces []storagebudget.RegisteredWorkspace
}

func storagePolicyEffectiveKey(s storagePolicyEffectiveSet, dict storageOwnerCatalogV2) ([]byte, error) {
	if !s.Policy.valid() || len(s.Workspaces) > storagebudget.MaxWorkspaces {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 488 + 2 + 1024*264}
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(s.Policy))
	w.u16(uint16(len(s.Workspaces)))
	for i, row := range s.Workspaces {
		if row.ID == (storagebudget.ID{}) || row.Generation == 0 || !storageOwnerLimitsValid(row.Limits) || i > 0 && bytes.Compare(s.Workspaces[i-1].ID[:], row.ID[:]) >= 0 {
			return nil, storagebudget.ErrBounds
		}
		found := false
		for _, original := range dict.Workspaces {
			if original.Definition.ID == row.ID {
				if found || original.Definition.Generation != row.Generation {
					return nil, storagebudget.ErrBounds
				}
				found = true
			}
		}
		if !found {
			return nil, storagebudget.ErrBounds
		}
		w.put(row.ID[:])
		w.u64(row.Generation)
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(row.Limits))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	return w.raw, nil
}

func storagePolicyLimitsBytes(l storagebudget.Limits) []byte {
	w := storageEnrollmentPeakWriter{max: 240}
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(l))
	return w.raw
}

// Build the one closed section2 container from factual inputs. Normalization
// interns only exact expanded sets and exact240-byte values. No tickets are
// predicted here: owed-terminal allowances belong to the pure closure check.
func storagePolicyBuildEffectiveTable(tickets []storagebudget.Ticket, sets []storagePolicyEffectiveSet, dict storageOwnerCatalogV2) ([]byte, []storagePolicyEffectiveSet, error) {
	if len(sets) == 0 || len(sets) > 66 {
		return nil, nil, storagebudget.ErrBounds
	}
	ticketRaw, err := storageEnrollmentPeakWriteTickets(tickets)
	if err != nil {
		return nil, nil, err
	}
	type keyed struct {
		key []byte
		set storagePolicyEffectiveSet
	}
	keys := make([]keyed, 0, len(sets))
	for _, s := range sets {
		key, e := storagePolicyEffectiveKey(s, dict)
		if e != nil {
			return nil, nil, e
		}
		s.Workspaces = append([]storagebudget.RegisteredWorkspace{}, s.Workspaces...)
		keys = append(keys, keyed{key, s})
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i].key, keys[j].key) < 0 })
	canonical := make([]storagePolicyEffectiveSet, 0, len(keys))
	pool := map[string]storagebudget.Limits{}
	for i, k := range keys {
		if i > 0 && bytes.Equal(keys[i-1].key, k.key) {
			continue
		}
		canonical = append(canonical, k.set)
		for _, row := range k.set.Workspaces {
			pool[string(storagePolicyLimitsBytes(row.Limits))] = row.Limits
		}
	}
	poolKeys := make([]string, 0, len(pool))
	for key := range pool {
		poolKeys = append(poolKeys, key)
	}
	sort.Strings(poolKeys)
	if len(poolKeys) > 66*1024 {
		return nil, nil, storagebudget.ErrBounds
	}
	poolIndex := map[string]uint32{}
	for i, key := range poolKeys {
		poolIndex[key] = uint32(i)
	}
	sw := storageEnrollmentPeakWriter{max: 35143680}
	for _, s := range canonical {
		storageEnrollmentPeakWriteFixed(&sw, reflect.ValueOf(s.Policy))
		sw.u16(uint16(len(s.Workspaces)))
		for _, row := range s.Workspaces {
			index, e := storageEnrollmentDictionaryIndex(dict, 3, row.ID)
			if e != nil {
				return nil, nil, e
			}
			sw.u16(index)
			sw.u32(poolIndex[string(storagePolicyLimitsBytes(row.Limits))])
		}
	}
	if sw.bad {
		return nil, nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 35143680}
	w.put([]byte("DSKEFS1\x00"))
	w.u16(1)
	w.u16(0)
	w.u16(uint16(len(tickets)))
	w.u16(uint16(len(canonical)))
	w.u32(uint32(len(poolKeys)))
	w.u32(uint32(len(ticketRaw)))
	w.u32(uint32(len(sw.raw)))
	total := uint64(32) + uint64(len(ticketRaw)) + 240*uint64(len(poolKeys)) + uint64(len(sw.raw))
	if total > 35143680 {
		return nil, nil, storagebudget.ErrBounds
	}
	w.u32(uint32(total))
	w.put(ticketRaw)
	for _, key := range poolKeys {
		w.put([]byte(key))
	}
	w.put(sw.raw)
	if w.bad || uint64(len(w.raw)) != total {
		return nil, nil, storagebudget.ErrBounds
	}
	return w.raw, canonical, nil
}

func storagePolicyReadEffectiveTable(raw []byte, count uint32, dict storageOwnerCatalogV2) ([]storagebudget.Ticket, []storagePolicyEffectiveSet, error) {
	fail := func() ([]storagebudget.Ticket, []storagePolicyEffectiveSet, error) {
		return nil, nil, storagebudget.ErrBounds
	}
	if len(raw) < 32 || len(raw) > 35143680 {
		return fail()
	}
	r := storageEnrollmentPeakReader{raw: raw}
	if !bytes.Equal(r.take(8), []byte("DSKEFS1\x00")) || r.u16() != 1 || r.u16() != 0 {
		return fail()
	}
	nt, ns, values := int(r.u16()), int(r.u16()), r.u32()
	if values > 66*1024 {
		return fail()
	}
	nu := int(values)
	tn, sn, total := uint64(r.u32()), uint64(r.u32()), uint64(r.u32())
	if uint32(nt) != count || nt > 4224 || ns < 1 || ns > 66 || nu > 66*1024 || total != uint64(len(raw)) || 32+tn+240*uint64(nu)+sn != total {
		return fail()
	}
	tickets, err := storageEnrollmentPeakReadTickets(r.take(int(tn)), uint32(nt))
	if err != nil {
		return nil, nil, err
	}
	pool := make([]storagebudget.Limits, nu)
	for i := range pool {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&pool[i]).Elem())
		if !storageOwnerLimitsValid(pool[i]) {
			return fail()
		}
		if i > 0 && bytes.Compare(storagePolicyLimitsBytes(pool[i-1]), storagePolicyLimitsBytes(pool[i])) >= 0 {
			return fail()
		}
	}
	sets := make([]storagePolicyEffectiveSet, ns)
	start := r.pos
	for i := range sets {
		storageEnrollmentPeakReadFixed(&r, reflect.ValueOf(&sets[i].Policy).Elem())
		n := int(r.u16())
		if n > 1024 {
			return fail()
		}
		sets[i].Workspaces = make([]storagebudget.RegisteredWorkspace, n)
		for j := range sets[i].Workspaces {
			index, value := int(r.u16()), uint64(r.u32())
			if index >= len(dict.Workspaces) || value >= uint64(len(pool)) {
				return fail()
			}
			d := dict.Workspaces[index].Definition
			sets[i].Workspaces[j] = storagebudget.RegisteredWorkspace{ID: d.ID, Generation: d.Generation, Limits: pool[value]}
		}
	}
	if !r.done() || uint64(r.pos-start) != sn {
		return fail()
	}
	// Canonical regeneration also rejects unused values, duplicate/nonordered
	// sets, duplicate or foreign/generation-mismatched membership and padding.
	canonical, owned, err := storagePolicyBuildEffectiveTable(tickets, sets, dict)
	if err != nil || !bytes.Equal(canonical, raw) {
		return fail()
	}
	return tickets, owned, nil
}

func storagePolicySetIndex(wanted storagePolicyEffectiveSet, sets []storagePolicyEffectiveSet, dict storageOwnerCatalogV2) (uint16, error) {
	key, err := storagePolicyEffectiveKey(wanted, dict)
	if err != nil {
		return 0, err
	}
	for i, s := range sets {
		other, e := storagePolicyEffectiveKey(s, dict)
		if e != nil {
			return 0, e
		}
		if bytes.Equal(key, other) {
			return uint16(i), nil
		}
	}
	return 0, storagebudget.ErrBounds
}

// A retained pair has one full expanded effective set. Legacy originals derive
// their own immutable admission rows, never the current editable values.
func storagePolicyProjectionSet(p storageEnrollmentPeakProjection) storagePolicyEffectiveSet {
	if p.Native != nil {
		return p.Native.Effective
	}
	if p.Policy != nil {
		out := p.Policy.Effective
		out.Workspaces = append([]storagebudget.RegisteredWorkspace{}, out.Workspaces...)
		return out
	}
	out := storagePolicyEffectiveSet{Policy: p.Catalog.Policy, Workspaces: []storagebudget.RegisteredWorkspace{}}
	for _, row := range p.Catalog.Workspaces {
		d := row.Definition
		out.Workspaces = append(out.Workspaces, storagebudget.RegisteredWorkspace{ID: d.ID, Generation: d.Generation, Limits: d.Limits})
	}
	sort.Slice(out.Workspaces, func(i, j int) bool { return bytes.Compare(out.Workspaces[i].ID[:], out.Workspaces[j].ID[:]) < 0 })
	return out
}
func storagePolicyViewProjectionPair(v storageEnrollmentPeakView, p storageEnrollmentPeakProjection, dict storageOwnerCatalogV2) bool {
	if v.Effective == nil {
		return false
	}
	a, err := storagePolicyEffectiveKey(*v.Effective, dict)
	if err != nil {
		return false
	}
	b, err := storagePolicyEffectiveKey(storagePolicyProjectionSet(p), dict)
	return err == nil && bytes.Equal(a, b)
}

// Version3 has no unpaired extra effective set. Each set is retained by a
// current or original View, and every original projection is checked against
// its own View. Complete-key equality alone interns a shared representation.
func storagePolicyEvidenceSets(e storageEnrollmentPeakEvidence) bool {
	if len(e.EffectiveSets) == 0 || len(e.EffectiveSets) > 66 || int(e.Checkpoint.View) >= len(e.Views) {
		return false
	}
	pairedViews := make([]bool, len(e.Views))
	pairedProjections := make([]bool, len(e.Projections))
	pairedViews[e.Checkpoint.View] = true
	for _, o := range e.Board.Origins {
		if int(o.View) >= len(e.Views) || int(o.Projection) >= len(e.Projections) {
			return false
		}
		pairedViews[o.View] = true
		pairedProjections[o.Projection] = true
	}
	used := make([]bool, len(e.EffectiveSets))
	for i, v := range e.Views {
		if !pairedViews[i] {
			return false
		}
		if v.Effective == nil {
			return false
		}
		i, err := storagePolicySetIndex(*v.Effective, e.EffectiveSets, e.Inner.Catalog)
		if err != nil {
			return false
		}
		used[i] = true
	}
	for _, yes := range used {
		if !yes {
			return false
		}
	}
	matches := 0
	for i, p := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(p) == e.Before {
			pairedProjections[i] = true
			matches++
			if !storagePolicyViewProjectionPair(e.Views[e.Checkpoint.View], p, e.Inner.Catalog) {
				return false
			}
		}
	}
	for _, yes := range pairedProjections {
		if !yes {
			return false
		}
	}
	return matches == 1
}

// The new closed variant resolves its set reference before the original full
// ViewCheckpoint validation. Legacy decoding never accepts or invents a set.
func storagePolicyWriteViews(views []storageEnrollmentPeakView, c storageOwnerCatalogV2, tickets []storagebudget.Ticket, sets []storagePolicyEffectiveSet) ([]byte, error) {
	if sets == nil {
		return nil, storagebudget.ErrBounds
	}
	return storageEnrollmentPeakWriteViewsVariant(views, c, tickets, sets)
}
func storagePolicyReadViews(raw []byte, count uint32, c storageOwnerCatalogV2, tickets []storagebudget.Ticket, sets []storagePolicyEffectiveSet) ([]storageEnrollmentPeakView, error) {
	if sets == nil {
		return nil, storagebudget.ErrBounds
	}
	views, err := storageEnrollmentPeakReadViewsVariant(raw, count, c, tickets, sets)
	if err != nil {
		return nil, err
	}
	canonical, err := storagePolicyWriteViews(views, c, tickets, sets)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, storagebudget.ErrBounds
	}
	return views, nil
}

// Writers accept already derived, ordered bounded DTOs. They do not reorder,
// deduplicate, discover native objects, measure, issue permits or repair input.
func storageEnrollmentPeakEncodeScope(s storageEnrollmentPeakScope) ([]byte, error) {
	if _, err := storageEnrollmentPeakDecodeScope(s[:]); err != nil {
		return nil, err
	}
	return append([]byte{}, s[:]...), nil
}
func storageEnrollmentPeakWriteTickets(tickets []storagebudget.Ticket) ([]byte, error) {
	if len(tickets) > 4224 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 35143680}
	var previous []byte
	for i, t := range tickets {
		body, err := storageEnrollmentPeakWriteTicket(t)
		if err != nil || i > 0 && bytes.Compare(previous, body) >= 0 {
			return nil, storagebudget.ErrBounds
		}
		start := len(w.raw)
		w.u16(uint16(i))
		w.u16(uint16(len(body)))
		digest := storageEnrollmentPeakHash("desk-enrollment-ticket-e1", body)
		w.put(digest[:])
		w.put(body)
		w.put(make([]byte, (8-(len(w.raw)-start)%8)%8))
		previous = body
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageEnrollmentPeakReadTickets(w.raw, uint32(len(tickets))); err != nil {
		return nil, err
	}
	return w.raw, nil
}
func storageEnrollmentPeakWriteViews(views []storageEnrollmentPeakView, c storageOwnerCatalogV2, tickets []storagebudget.Ticket) ([]byte, error) {
	return storageEnrollmentPeakWriteViewsVariant(views, c, tickets, nil)
}
func storageEnrollmentPeakWriteViewsVariant(views []storageEnrollmentPeakView, c storageOwnerCatalogV2, tickets []storagebudget.Ticket, sets []storagePolicyEffectiveSet) ([]byte, error) {
	if len(views) == 0 || len(views) > 66 {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageEnrollmentPeakWriteTickets(tickets); err != nil {
		return nil, err
	}
	index := map[string]uint16{}
	for i, t := range tickets {
		body, err := storageEnrollmentPeakWriteTicket(t)
		if err != nil {
			return nil, err
		}
		index[string(body)] = uint16(i)
	}
	w := storageEnrollmentPeakWriter{max: 1081344}
	limits := [4]int{128, 512, 4096, 1024}
	for _, v := range views {
		start := len(w.raw)
		w.u64(v.Revision)
		w.u64(v.Epoch)
		if v.Maintenance {
			w.u8(1)
		} else {
			w.u8(0)
		}
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(v.Policy))
		for k, refs := range v.DefinitionRefs {
			if len(refs) > limits[k] {
				return nil, storagebudget.ErrBounds
			}
			w.u16(uint16(len(refs)))
			for _, ref := range refs {
				w.u16(ref)
			}
		}
		for _, slot := range v.Slots {
			body, err := storageEnrollmentPeakWriteTicket(slot.Ticket)
			if err != nil {
				return nil, err
			}
			ref, ok := index[string(body)]
			if !ok {
				return nil, storagebudget.ErrBounds
			}
			w.u16(ref)
			storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(slot.PersistedApplied))
			w.u64(slot.PersistedNextSequence)
		}
		if sets != nil {
			if v.Effective == nil {
				return nil, storagebudget.ErrBounds
			}
			index, err := storagePolicySetIndex(*v.Effective, sets, c)
			if err != nil {
				return nil, err
			}
			w.u16(index)
		} else if v.Effective != nil {
			return nil, storagebudget.ErrBounds
		}
		if w.bad || len(w.raw)-start > 16384 {
			return nil, storagebudget.ErrBounds
		}
	}
	if _, err := storageEnrollmentPeakReadViewsVariant(w.raw, uint32(len(views)), c, tickets, sets); err != nil {
		return nil, err
	}
	return w.raw, nil
}
func storageEnrollmentPeakWriteCheckpointV1(p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope) ([]byte, error) {
	raw, _, err := storageEnrollmentPeakWriteSelectedCheckpoint(p, c, views, s)
	return raw, err
}

// Return the strict selected projection only to this synchronous writer. The
// peer-aware writer can finish its own frame without decoding it a second time.
func storageEnrollmentPeakWriteSelectedCheckpoint(p storageEnrollmentPeakCheckpointProjection, c storageOwnerCatalogV2, views []storageEnrollmentPeakView, s storageEnrollmentPeakScope) ([]byte, storageEnrollmentPeakCheckpointProjection, error) {
	if p.Q == nil || int(p.View) >= len(views) || storageEnrollmentPeakQValidateCheckpoint(p.Q) != nil {
		return nil, storageEnrollmentPeakCheckpointProjection{}, storagebudget.ErrBounds
	}
	q := p.Q
	w := storageEnrollmentPeakWriter{max: 2097152}
	w.u64(q.Generation)
	w.u64(q.Epoch)
	if q.Maintenance {
		w.u8(1)
	} else {
		w.u8(0)
	}
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(views[p.View].Policy))
	storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(q.Installation))
	refs := [][]uint16{p.RootRefs, p.WorkspaceRefs, p.FilesystemRefs, p.ConstraintRefs}
	arrays := []reflect.Value{reflect.ValueOf(q.Roots), reflect.ValueOf(q.Workspaces), reflect.ValueOf(q.Filesystems), reflect.ValueOf(q.CapacityConstraints)}
	for kind, array := range arrays {
		if len(refs[kind]) > array.Len() {
			return nil, storageEnrollmentPeakCheckpointProjection{}, storagebudget.ErrBounds
		}
		w.u16(uint16(len(refs[kind])))
		next := 0
		for i := 0; i < array.Len(); i++ {
			row := array.Index(i)
			if row.FieldByName("ID").IsZero() {
				continue
			}
			if next >= len(refs[kind]) {
				return nil, storageEnrollmentPeakCheckpointProjection{}, storagebudget.ErrBounds
			}
			w.u16(uint16(i))
			w.u16(refs[kind][next])
			next++
			if kind < 2 {
				storageEnrollmentPeakWriteFixed(&w, row.FieldByName("Known"))
				storageEnrollmentPeakWriteFixed(&w, row.FieldByName("Usage"))
			}
		}
		if next != len(refs[kind]) {
			return nil, storageEnrollmentPeakCheckpointProjection{}, storagebudget.ErrBounds
		}
	}
	for _, a := range q.Applied {
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(a))
	}
	w.u8(p.Bank)
	for _, d := range p.Checkpoints {
		w.put(d[:])
	}
	for _, pair := range p.Tickets {
		for _, d := range pair {
			w.put(d[:])
		}
	}
	for _, bank := range p.TicketBanks {
		w.u8(bank)
	}
	w.u16(p.View)
	if w.bad {
		return nil, storageEnrollmentPeakCheckpointProjection{}, storagebudget.ErrBounds
	}
	decoded, err := storageEnrollmentPeakReadCheckpointV1(w.raw, c, views, s)
	if err != nil {
		return nil, storageEnrollmentPeakCheckpointProjection{}, err
	}
	if !reflect.DeepEqual(q, decoded.Q) {
		return nil, storageEnrollmentPeakCheckpointProjection{}, storagebudget.ErrBounds
	}
	return w.raw, decoded, nil
}
func storageEnrollmentPeakWriteCensus(f storageEnrollmentPeakQCensus, q *storageEnrollmentPeakQCheckpoint, phase byte) ([]byte, error) {
	if q == nil || len(f.Roots) > 4096 || len(f.Workspaces) > 1024 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 2097152}
	w.put(f.Evidence[:])
	w.u16(uint16(len(f.Roots)))
	for _, v := range f.Roots {
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(v))
	}
	w.u16(uint16(len(f.Workspaces)))
	for _, v := range f.Workspaces {
		storageEnrollmentPeakWriteFixed(&w, reflect.ValueOf(v))
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageEnrollmentPeakReadCensus(w.raw, q, phase); err != nil {
		return nil, err
	}
	return w.raw, nil
}
func storageEnrollmentPeakWriteWitnesses(witnesses []storageEnrollmentPeakWitness, installation storagebudget.ID, binding storagebudget.Digest) ([]byte, error) {
	if len(witnesses) > 128 {
		return nil, storagebudget.ErrBounds
	}
	w := storageEnrollmentPeakWriter{max: 2113536}
	for _, v := range witnesses {
		h := storageEnrollmentPeakWriter{max: 128}
		h.u16(v.Slot)
		h.u8(v.Bank)
		h.u8(v.Version)
		h.u64(v.Generation)
		h.put(v.Operation[:])
		h.put(v.Digest[:])
		h.u16(v.Origin)
		if v.Selected {
			h.u8(1)
		} else {
			h.u8(0)
		}
		h.u8(v.Phase)
		h.u8(v.Branch)
		h.u8(0)
		if h.bad || len(v.Page) != 16384 {
			return nil, storagebudget.ErrBounds
		}
		header := make([]byte, 128)
		copy(header, h.raw)
		w.put(header)
		w.put(v.Page)
	}
	if w.bad {
		return nil, storagebudget.ErrBounds
	}
	if _, err := storageEnrollmentPeakReadWitnesses(w.raw, uint32(len(witnesses)), installation, binding); err != nil {
		return nil, err
	}
	return w.raw, nil
}
