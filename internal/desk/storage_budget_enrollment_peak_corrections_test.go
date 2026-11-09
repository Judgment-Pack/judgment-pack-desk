package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func epReplan(s *storageEnrollmentPeakScope) {
	groups := epGroups(s.path(), [2]uint64{s.final(0), s.final(1)}, s[322], s[323], s[324], s[325])
	var parents [2]storagebudget.CapacityVector
	var targets [10]uint64
	for _, g := range groups {
		v := epEstimate(g)
		parents[g.Parent-1].Bytes += v.Bytes
		parents[g.Parent-1].Inodes += v.Inodes
		targets[g.Target-1] += v.Bytes
	}
	epSet16(s[1632:], uint16(len(groups)))
	epSet64(s[384:], parents[0].Bytes+parents[1].Bytes)
	epSet64(s[392:], parents[0].Inodes+parents[1].Inodes)
	for i := 0; i < 8; i++ {
		epSet64(s[1380+32*i:], targets[i])
	}
	for i := 0; i < int(s[326]); i++ {
		b := s[440+80*i : 520+80*i]
		var v storagebudget.CapacityVector
		for p := 0; p < 2; p++ {
			if b[24]&(1<<p) != 0 {
				v.Bytes += parents[p].Bytes
				v.Inodes += parents[p].Inodes
			}
		}
		epSet64(b[32:], v.Bytes)
		epSet64(b[40:], v.Inodes)
	}
	epScopeHash(s)
}
func epNormal(t *testing.T, e storageEnrollmentPeakEvidence, phase byte, sequence uint64, previous storagebudget.Digest, record storageOwnerRecordV2, claim storageOwnerClaimV2) []byte {
	t.Helper()
	m := epMarker(e, phase, previous)
	raw, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 2, BeforeCodec: func() byte {
		if e.Scope.path() == 1 {
			return 0
		}
		return 1
	}(), Sequence: sequence, Scope: e.Scope, Marker: &m}, record, claim)
	if err != nil {
		t.Fatal("normal", phase, err)
	}
	return raw
}
func epDecode(t *testing.T, f epComplete) storageEnrollmentPeakEvidence {
	t.Helper()
	e, err := storageEnrollmentPeakDecodeEvidence(f.raw, f.record, f.claim)
	if err != nil {
		t.Fatal("fixture evidence", err)
	}
	return e
}
func epRelayout(e *storageEnrollmentPeakEvidence) {
	last := uint64(65536)
	for i := range e.Sections {
		last = (last + 7) &^ uint64(7)
		s := &e.Sections[i]
		s.Offset = last
		s.Length = uint64(len(s.Raw))
		s.Digest = sha256.Sum256(s.Raw)
		last += s.Length
	}
	e.Packed = last - 65536
}
func epPeerDispatch(e storageEnrollmentPeakEvidence) [2][]byte {
	d0 := epDispatch(e, 0, 1, storagebudget.Digest{})
	d1 := epDispatch(e, 1, 2, sha256.Sum256(d0))
	d2 := epDispatch(e, 2, 3, sha256.Sum256(d1))
	d3 := epDispatch(e, 3, 4, sha256.Sum256(d2))
	return [2][]byte{d2, d3}
}

func TestStorageEnrollmentPeakPrefixOriginalAndArmedJoins(t *testing.T) {
	f := epEvidence(t, 1)
	e := epDecode(t, f)
	old := ownerAdvisoryWire(f.before)
	snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: sha256.Sum256(old), Inner: epHash("desk-enrollment-original-v3-inner-peak-v1", old[64:64+int(binary.LittleEndian.Uint32(old[24:]))]), Before: old}
	prefix := storageEnrollmentPeakPrefix{Catalog: epJSON(f.after), Projection: f.sections[4], Tickets: f.sections[2], Views: f.sections[3], Checkpoint: f.sections[6], TicketCount: 64, ViewCount: 1}
	for _, captured := range []bool{false, true} {
		t.Run(fmt.Sprint(captured), func(t *testing.T) {
			scope := e.Scope
			kind, sequence := byte(0), uint64(2)
			if !captured {
				scope[321] = 0
				clear(scope[1080:1220])
			} else {
				kind = 1
				sequence = 3
			}
			raw, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: kind, BeforeCodec: 0, Sequence: sequence, Scope: scope, Snapshot: &snapshot}, f.record, f.claim)
			if err != nil {
				t.Fatal(err)
			}
			original := epOriginal(f, e)
			for i := 0; i < 4; i++ {
				original.Extents[i] = 0
				if !captured {
					original.Targets[i] = storageOwnerPersistentObject{}
				}
			}
			input := storageEnrollmentPeakComparisonInput{Prefix: prefix, Original: original, CurrentCheckpoints: original.Checkpoints, CurrentTickets: original.Tickets}
			if captured {
				uncaptured := scope
				uncaptured[321] = 0
				clear(uncaptured[1080:1220])
				fence, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: 2, Scope: uncaptured, Snapshot: &snapshot}, f.record, f.claim)
				if err != nil {
					t.Fatal(err)
				}
				input.Catalogs = [2][]byte{fence, raw}
			} else {
				input.Catalogs = [2][]byte{raw, old}
			}
			if _, err := storageEnrollmentPeakCompare(input, f.record, f.claim); err != nil {
				t.Fatal("full prefix", err)
			}
			for _, mutation := range []string{"Q0", "ticket", "host", "ready", "project", "view", "projection", "source", "extent", "current-Q"} {
				bad := input
				switch mutation {
				case "Q0":
					bad.Original.Checkpoints[0] = append([]byte{}, original.Checkpoints[0]...)
					bad.Original.Checkpoints[0][200] ^= 1
				case "ticket":
					bad.Original.Tickets[0] = append([]byte{}, original.Tickets[0]...)
					bad.Original.Tickets[0][200] ^= 1
				case "host":
					bad.Original.Hosts[0][0] = bytes.Repeat([]byte{1}, 16384)
				case "ready":
					bad.Original.Ready = storagebudget.Digest{}
				case "project":
					bad.Original.Project.Object++
				case "view":
					bad.Prefix.Views = append([]byte{}, prefix.Views...)
					bad.Prefix.Views[0]++
				case "projection":
					bad.Prefix.Projection = append([]byte{}, prefix.Projection...)
					bad.Prefix.Projection[0]++
				case "source":
					bad.Prefix.Catalog = bytes.Replace(prefix.Catalog, []byte(`"Version":1`), []byte(`"Version":3`), 1)
				case "extent":
					bad.Original.Extents[4]--
				case "current-Q":
					bad.CurrentCheckpoints[1] = f.cp[1]
				}
				if _, err := storageEnrollmentPeakCompare(bad, f.record, f.claim); err == nil {
					t.Fatal("prefix mutation", mutation)
				}
			}
		})
	}
	completedFixture := epEvidence(t, 2)
	completed := epDecode(t, completedFixture)
	if !storageEnrollmentPeakJoinArmed(completed, e) {
		t.Fatal("exact referenced Armed image")
	}
	normal := epNormal(t, completed, 3, 6, storagebudget.Digest{1}, f.record, f.claim)
	input := storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{normal, normal}, Evidence: completed.Raw, ArmedEvidence: e.Raw, Dispatch: epPeerDispatch(e), CurrentCheckpoints: [2][]byte{f.cp[2], f.cp[3]}, CurrentTickets: f.tickets, Original: epOriginal(f, completed)}
	if _, err := storageEnrollmentPeakCompare(input, f.record, f.claim); err != nil {
		t.Fatal("Completed joins referenced Armed", err)
	}
	input.ArmedEvidence = nil
	if _, err := storageEnrollmentPeakCompare(input, f.record, f.claim); err == nil {
		t.Fatal("missing Armed")
	}
	for _, mutation := range []string{"F", "length", "before", "shape", "generation", "previous", "bank", "source"} {
		bad := completed
		switch mutation {
		case "F":
			bad.CensusDigest[0]++
		case "length":
			bad.Packed++
		case "before":
			bad.Before[0]++
		case "shape":
			bad.Sections[7].Raw = append(append([]byte{}, bad.Sections[7].Raw...), 0)
		case "generation":
			bad.Generation++
		case "previous":
			bad.Previous[0]++
		case "bank":
			bad.Bank = e.Bank
		case "source":
			bad.Sections[8].Raw = append([]byte{}, bad.Sections[8].Raw...)
			bad.Sections[8].Raw[len(bad.Sections[8].Raw)-1] = 1
		}
		if storageEnrollmentPeakJoinArmed(bad, e) {
			t.Fatal("Armed join mutation", mutation)
		}
	}
	diagnostic := completed
	diagnostic.Sections[8].Raw = append([]byte{}, completed.Sections[8].Raw...)
	offset := 174 + 24*int(e.Scope[326])
	diagnostic.Sections[8].Raw[offset+38] = 3
	epSet64(diagnostic.Sections[8].Raw[offset+107:], 99)
	if !storageEnrollmentPeakJoinArmed(diagnostic, e) {
		t.Fatal("diagnostic state/generation are not immutable authority")
	}
}

func epPeakProjection(c, dict storageOwnerCatalogV2, outer []byte) []byte {
	raw := epProjection(c, dict, outer)
	w := epBytes{}
	w.Write(raw[:len(raw)-68])
	w.fixed(uint16(65535))
	w.data(outer[:8320])
	b := append([]byte{}, w.Bytes()...)
	b[2] = 1
	epSet32(b[4:], uint32(len(b)))
	return b
}
func epExisting(t *testing.T, path, phase byte) (epComplete, epComplete, []byte) {
	return epExistingExtents(t, path, phase, false)
}
func epExistingExtents(t *testing.T, path, phase byte, unequal bool) (epComplete, epComplete, []byte) {
	t.Helper()
	beforeExtents := [2]uint64{262144, 262144}
	if unequal {
		beforeExtents[1] = 327680
	}
	prior := epEvidenceExtents(t, 2, beforeExtents)
	previous := epDecode(t, prior)
	oldOuter := epNormal(t, previous, 3, 6, storagebudget.Digest{1}, prior.record, prior.claim)
	f := epComplete{before: ownerCatalogClone(prior.after), record: prior.record, claim: prior.claim, source: prior.source, tickets: prior.tickets}
	f.after = ownerCatalogClone(f.before)
	f.after.Sequence = 3
	f.after.Transition = storagebudget.ID{10}
	copy(f.after.PreviousCatalogDigest[:], oldOuter[32:64])
	f.after.ExpectedQRevision = 14
	f.after.BeforeDefinitionIDs.Roots = []storagebudget.ID{}
	for _, r := range f.before.Roots {
		f.after.BeforeDefinitionIDs.Roots = append(f.after.BeforeDefinitionIDs.Roots, r.Definition.ID)
	}
	parent := f.before.Roots[7]
	child := parent
	child.Role = 6
	child.Definition.ID = storagebudget.ID{9}
	child.Definition.Locator = "captured-child:9"
	child.Source = storageOwnerRootSource{Kind: 8, OwnerKey: append([]byte{}, parent.Source.OwnerKey...), Path: []byte{}, ParentRoot: parent.Definition.ID, Relative: []byte(".desk-private"), ControlIndex: math.MaxUint32}
	child.DirectoryObject = uint32(len(f.after.Objects))
	child.Membership.ExistingObjects = []uint32{}
	f.after.Objects = append(f.after.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: math.MaxUint32, Name: []byte{}, Persistent: epNative(101, 1), OwnerClass: 2, ModeClass: 1})
	child.Definition.Identity = ownerCatalogIdentity(f.after, child)
	d := child.Definition
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range f.after.Constraints {
		constraints = append(constraints, v.Definition)
	}
	var err error
	child.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, f.after.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	f.after.Roots = append(f.after.Roots, child)
	epSealDefinitions(&f.after)
	f.q[0] = prior.q[3]
	f.cp[0] = prior.cp[3]
	view, viewRaw := epView(f.before, f.q[0])
	final := beforeExtents
	if path == 3 {
		final = [2]uint64{524288, 524288}
		if unequal {
			final[1] = 589824
		}
	}
	f.scope = epScope(path, true, f.record, beforeExtents, final)
	f.scope[24] = 10
	f.scope[322] = 0
	f.scope[323] = 1
	f.scope[324] = 1
	f.scope[325] = 1
	epSet64(f.scope[1624:], 6)
	bd := sha256.Sum256(oldOuter)
	copy(f.scope[88:120], bd[:])
	bd = sha256.Sum256(f.cp[0])
	copy(f.scope[120:152], bd[:])
	bd = sha256.Sum256(viewRaw)
	copy(f.scope[152:184], bd[:])
	copy(f.scope[440:456], f.after.Constraints[0].Definition.ID[:])
	epReplan(&f.scope)
	for i := 1; i <= 3; i++ {
		q := *f.q[i-1]
		q.Generation++
		if i == 1 {
			q.Epoch++
			q.Maintenance = true
		}
		if i == 2 {
			epInto(&q.Roots[8], child.Definition)
		}
		if i == 3 {
			epMeasuredQ(&q)
		}
		f.q[i] = &q
		f.cp[i] = epQBytes(&q, f.record)
	}
	f.sections[0] = append([]byte{}, prior.sections[0]...)
	controls := [4]storageOwnerPersistentObject{}
	for i := range controls {
		controls[i] = f.scope.target(byte(i + 1))
	}
	f.sections[1], err = storageEnrollmentPeakEncodeInner(storageEnrollmentPeakInner{Catalog: f.after, Controls: controls}, f.record, f.claim)
	if err != nil {
		t.Fatal(err)
	}
	f.sections[2] = append([]byte{}, prior.sections[2]...)
	f.sections[3] = viewRaw
	_ = view
	f.sections[4] = epPeakProjection(f.before, f.after, oldOuter)
	f.sections[5] = []byte{}
	f.sections[6] = epCheckpoint(f.before, f.q[0], f.cp[0], f.tickets)
	bankOffset := len(f.sections[6]) - 4227
	f.sections[6][bankOffset] = 1
	low := sha256.Sum256(prior.cp[2])
	high := sha256.Sum256(prior.cp[3])
	copy(f.sections[6][bankOffset+1:], low[:])
	copy(f.sections[6][bankOffset+33:], high[:])
	f.sections[7] = epCensus(f.q[2], phase != 0)
	registration := storagebudget.Registration{Roots: []storagebudget.Root{}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	var root storagebudget.Root
	epInto(&root, child.Definition)
	registration.Roots = append(registration.Roots, root)
	rd := sha256.Sum256(epJSON(registration))
	o := epBytes{}
	o.fixed(f.record.Installation)
	o.fixed(f.record.QBinding)
	o.Write(f.scope[24:40])
	o.fixed(f.source.Project)
	o.fixed(rd)
	origin := epHash("desk-enrollment-origin-e1", o.Bytes())
	qd := [3]storagebudget.Digest{sha256.Sum256(f.cp[1]), sha256.Sum256(f.cp[2]), sha256.Sum256(f.cp[3])}
	f.sections[8] = epBoard(f.scope, f.source, phase, qd, origin)
	groupEnd := 174 + 24*int(f.scope[326]) + len(epGroups(path, final, 0, 1, 1, 1))*115
	tail := epBytes{}
	tail.fixed(previous.Bridge)
	tail.fixed(previous.AfterEpoch)
	tail.fixed(previous.AfterRevision)
	tail.fixed(sha256.Sum256(oldOuter))
	tail.fixed(sha256.Sum256(previous.Raw))
	copy(f.sections[8][groupEnd+4:], tail.Bytes())
	epSet16(f.sections[8][groupEnd+102:], 8)
	f.sections[8][groupEnd+110] = 8
	bank := byte(0)
	if path == 3 {
		bank = 1
	}
	generation := uint64(5)
	previousDigest := sha256.Sum256(previous.Raw)
	if path == 3 {
		if phase == 0 {
			bank = 0
		} else {
			generation = 7
			previousDigest = storagebudget.Digest{7}
		}
	}
	if phase == 2 {
		bank = 1 - bank
		generation++
		armed, _, _ := epExistingExtents(t, path, 1, unequal)
		previousDigest = sha256.Sum256(armed.raw)
	}
	e := storageEnrollmentPeakEvidence{Generation: generation, Extent: final[bank], Phase: phase, Bank: bank, Installation: f.record.Installation, QBinding: f.record.QBinding, Bridge: storagebudget.ID{10}, Previous: previousDigest, Record: previous.Record, Claim: previous.Claim, Ready: previous.Ready, Before: sha256.Sum256(oldOuter), BeforeInner: sha256.Sum256(epJSON(f.before)), AfterInner: sha256.Sum256(epJSON(f.after)), BeforeRevision: 13, BeforeEpoch: 2, AfterRevision: 16, AfterEpoch: 3, Controls: controls, Project: f.source.Project, RegistrationDigest: rd, CensusDigest: sha256.Sum256(f.sections[7]), CoveredEpoch: 3, Scope: f.scope}
	counts := [9]uint32{1, 1, 64, 1, 1, 0, 1, 1, 1}
	for i := range e.Sections {
		e.Sections[i] = storageEnrollmentPeakSection{Count: counts[i], Raw: f.sections[i]}
	}
	epRelayout(&e)
	if 65536+e.Packed > e.Extent {
		t.Fatal("existing actual fit", e.Packed, e.Extent)
	}
	f.evidence = e
	f.raw = epEvidenceBytes(e)
	return f, prior, oldOuter
}
func TestStorageEnrollmentPeakExistingBridgeTransitions(t *testing.T) {
	for _, path := range []byte{2, 3} {
		t.Run(fmt.Sprint(path), func(t *testing.T) {
			armed, prior, oldOuter := epExisting(t, path, 1)
			ae := epDecode(t, armed)
			completed, _, _ := epExisting(t, path, 2)
			ce := epDecode(t, completed)
			if !storageEnrollmentPeakJoinArmed(ce, ae) {
				t.Fatal("existing Armed/Completed")
			}
			snapshot := storageEnrollmentPeakSnapshot{Kind: 1, Full: sha256.Sum256(oldOuter), Inner: epHash("desk-enrollment-inner-catalog-peak-v1", prior.sections[1]), Evidence: prior.evidence.Controls[prior.evidence.Bank], Bank: prior.evidence.Bank, Extent: prior.evidence.Extent, EvidenceDigest: sha256.Sum256(prior.raw), Q3Revision: 13, Q3Epoch: 2, Before: oldOuter}
			fence, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 1, Sequence: 7, Scope: ae.Scope, Snapshot: &snapshot}, armed.record, armed.claim)
			if err != nil {
				t.Fatal("compact prior snapshot", err)
			}
			if _, _, err := storageEnrollmentPeakDecodePair(oldOuter, fence, armed.record, armed.claim); err != nil {
				t.Fatal("new Fence / old Completed", err)
			}
			if storageEnrollmentPeakCompareSnapshot(func() storageEnrollmentPeakEnvelope {
				x, _ := storageEnrollmentPeakDecodeEnvelope(fence, armed.record, armed.claim, true)
				return x
			}(), epDecode(t, prior), armed.record, armed.claim) != nil {
				t.Fatal("exact prior endpoint")
			}
			original := epOriginal(armed, ae)
			original.Catalogs = [2][]byte{oldOuter, oldOuter}
			original.Checkpoints = [2][]byte{prior.cp[2], prior.cp[3]}
			for i := 0; i < 2; i++ {
				original.Extents[i] = 262144
			}
			prefix := storageEnrollmentPeakPrefix{Catalog: epJSON(armed.after), Projection: armed.sections[4], Tickets: armed.sections[2], Views: armed.sections[3], Checkpoint: armed.sections[6], TicketCount: 64, ViewCount: 1}
			prefixInput := storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{oldOuter, fence}, Prefix: prefix, PreviousEvidence: prior.raw, Original: original, CurrentCheckpoints: original.Checkpoints, CurrentTickets: original.Tickets}
			if _, err := storageEnrollmentPeakCompare(prefixInput, armed.record, armed.claim); err != nil {
				t.Fatal("existing complete prefix", err)
			}
			current := fence
			sequence := uint64(8)
			if path == 3 {
				stage, _, _ := epExisting(t, 3, 0)
				se := epDecode(t, stage)
				r := epNormal(t, se, 0, 8, func() storagebudget.Digest { var d storagebudget.Digest; copy(d[:], fence[32:64]); return d }(), armed.record, armed.claim)
				if _, bank, err := storageEnrollmentPeakDecodePair(r, fence, armed.record, armed.claim); err != nil || bank != 0 {
					t.Fatal("growth R", err)
				}
				rOriginal := original
				rOriginal.Extents[se.Bank] = se.Extent
				oldAttempt := epPeerDispatch(epDecode(t, prior))[1]
				enteredAttempt := epDispatch(se, 0, 5, sha256.Sum256(oldAttempt))
				rInput := storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{r, fence}, Evidence: stage.raw, PreviousEvidence: prior.raw, Dispatch: [2][]byte{oldAttempt, enteredAttempt}, Original: rOriginal, CurrentCheckpoints: original.Checkpoints, CurrentTickets: original.Tickets}
				if _, err := storageEnrollmentPeakCompare(rInput, armed.record, armed.claim); err != nil {
					t.Fatal("growth complete R/Q0", err)
				}
				current = r
				sequence = 9
			}
			previous := storagebudget.Digest{}
			copy(previous[:], current[32:64])
			prepared := epNormal(t, ae, 1, sequence, previous, armed.record, armed.claim)
			pBank := 0
			if path == 3 {
				pBank = 1
			}
			pair := [2][]byte{current, current}
			pair[pBank] = prepared
			if _, bank, err := storageEnrollmentPeakDecodePair(pair[0], pair[1], armed.record, armed.claim); err != nil || bank != pBank {
				t.Fatal("P phase/bank", err)
			}
			copy(previous[:], prepared[32:64])
			definitions := epNormal(t, ae, 2, sequence+1, previous, armed.record, armed.claim)
			dBank := 1 - pBank
			pair = [2][]byte{prepared, prepared}
			pair[dBank] = definitions
			if _, bank, err := storageEnrollmentPeakDecodePair(pair[0], pair[1], armed.record, armed.claim); err != nil || bank != dBank {
				t.Fatal("D phase/bank", err)
			}
			copy(previous[:], definitions[32:64])
			closed := epNormal(t, ce, 3, sequence+2, previous, armed.record, armed.claim)
			pair = [2][]byte{definitions, definitions}
			pair[pBank] = closed
			if _, bank, err := storageEnrollmentPeakDecodePair(pair[0], pair[1], armed.record, armed.claim); err != nil || bank != pBank {
				t.Fatal("C phase/bank", err)
			}
			// Cross-bridge Entered links the actual old Census attempt. New Q writes
			// alternate from original q=1, preserving original ticket banks unchanged.
			priorEvidence := epDecode(t, prior)
			oldDispatch := epPeerDispatch(priorEvidence)[1]
			entered := epDispatch(ae, 0, 5, sha256.Sum256(oldDispatch))
			begin := epDispatch(ae, 1, 6, sha256.Sum256(entered))
			register := epDispatch(ae, 2, 7, sha256.Sum256(begin))
			census := epDispatch(ae, 3, 8, sha256.Sum256(register))
			original.Extents[0] = ae.Scope.final(0)
			original.Extents[1] = ae.Scope.final(1)
			input := storageEnrollmentPeakComparisonInput{Catalogs: pair, Evidence: completed.raw, ArmedEvidence: armed.raw, PreviousEvidence: prior.raw, Dispatch: [2][]byte{register, census}, Original: original, CurrentCheckpoints: [2][]byte{armed.cp[3], armed.cp[2]}, CurrentTickets: armed.tickets}
			if _, err := storageEnrollmentPeakCompare(input, armed.record, armed.claim); err != nil {
				t.Fatal("existing complete combined C/Q3", err)
			}
			input.PreviousEvidence = nil
			if _, err := storageEnrollmentPeakCompare(input, armed.record, armed.claim); err == nil {
				t.Fatal("lost original endpoint")
			}
			bad := append([]byte{}, oldOuter[:8320]...)
			bad[64] = 0
			if _, err := storageEnrollmentPeakReadSnapshot(append(make([]byte, 256), bad...), ae.Scope, 2, armed.record, armed.claim); err == nil {
				t.Fatal("pending recursive old snapshot")
			}
		})
	}
}

func epRetired(t *testing.T, identity, later bool) (storageEnrollmentPeakEvidence, storageIntentRecord) {
	t.Helper()
	return epRetiredEvidence(t, epEvidence(t, 1), identity, later)
}

func epRetiredEvidence(t *testing.T, f epComplete, identity, later bool) (storageEnrollmentPeakEvidence, storageIntentRecord) {
	t.Helper()
	e := epDecode(t, f)
	d := f.before.Roots[0].Definition
	fs := f.before.Filesystems[0].Definition
	component := storagebudget.Component{Filesystem: d.Filesystem, FilesystemGeneration: fs.Generation, Root: d.ID, RootGeneration: d.Generation, Owner: d.Owner, Category: 4, Physical: storagebudget.Vector{Bytes: 512, Files: 2}, SettlementPhysical: storagebudget.Vector{Bytes: 134217728}, CapacityCount: d.CapacityCount, CapacityBinding: d.CapacityBinding}
	for i := 0; i < int(d.CapacityCount); i++ {
		component.Capacity[i] = storagebudget.CapacityPromise{Constraint: d.CapacityRefs[i].Constraint, Generation: d.CapacityRefs[i].Generation, Peak: storagebudget.CapacityVector{Bytes: 135266304, Inodes: 2}}
	}
	core := storageIntentRecord{Slot: 0, Generation: 3, Phase: storageIntentRetired, OperationID: storagebudget.ID{40}, PreviousPageDigest: storagebudget.Digest{1}, CASBinding: storagebudget.Digest{2}, AdmissionEpoch: 1, PolicyRevision: 1, RetirementEpoch: 1, NewLength: 100, Token: storagebudget.Token{Slot: 1, ID: storagebudget.ID{90}, Sequence: 2, Generation: 3}, PreviousToken: storagebudget.Token{Slot: 1, ID: storagebudget.ID{90}, Sequence: 2, Generation: 2}, Outcome: storageIntentAborted, Evidence: storagebudget.Digest{3}, Components: []storagebudget.Component{component}, RootIdentities: []storagebudget.Digest{d.Identity}, FilesystemIdentities: []storagebudget.Digest{fs.Identity}, Changes: []storagebudget.Change{{}}, Target: "target", Stage: "stage", Workflow: "handwritten-retirement", Operation: "retirement", Locators: []string{d.Locator}}
	var page []byte
	var err error
	version := byte(2)
	if identity {
		r := storageIdentityIntentTerminal(storageIdentityIntentState(storageIdentityAbortObserved), storageIntentRetired)
		r.Core.AdmissionEpoch = 1
		r.Core.RetirementEpoch = 1
		r.Core.Components = []storagebudget.Component{component}
		r.Core.RootIdentities = []storagebudget.Digest{d.Identity}
		r.Core.FilesystemIdentities = []storagebudget.Digest{fs.Identity}
		r.Core.Locators = []string{d.Locator}
		r.Core.Evidence, _ = storageOwnerDigest("desk-storage-identity-publication-v1", storageIdentityPublicationMaterial(r))
		core = r.Core
		version = 3
		page, err = storageEncodeIdentityIntent(r, f.record.Installation, f.record.QBinding)
	} else {
		page, err = storageEncodeIntent(core, f.record.Installation, f.record.QBinding)
	}
	if err != nil {
		t.Fatal("authored original retired", identity, err)
	}
	v := e.Views[0]
	slot := int(core.Token.Slot)
	if later {
		v.Slots[slot].PersistedApplied = storagebudget.Token{Slot: byte(slot), ID: storagebudget.ID{91}, Sequence: core.Token.Sequence + 1, Generation: 1}
		v.Slots[slot].PersistedNextSequence = core.Token.Sequence + 2
	} else {
		v.Slots[slot].PersistedApplied = core.Token
		v.Slots[slot].PersistedNextSequence = core.Token.Sequence + 1
		v.Slots[slot].Ticket = storagebudget.Ticket{Token: core.Token, State: storagebudget.Terminal, PolicyRevision: core.PolicyRevision, Workflow: storageHostWorkflow, Operation: storageIntentOperation(core), Owner: f.record.QBinding, Evidence: core.Evidence, Components: core.Components, Changes: core.Changes}
	}
	tickets := append([]storagebudget.Ticket{}, e.Tickets...)
	if !later {
		tickets = append(tickets, v.Slots[slot].Ticket)
	}
	sort.Slice(tickets, func(i, j int) bool { return bytes.Compare(epTicketBody(tickets[i]), epTicketBody(tickets[j])) < 0 })
	viewsRaw, err := storageEnrollmentPeakWriteViews([]storageEnrollmentPeakView{v}, e.Inner.Catalog, tickets)
	if err != nil {
		t.Fatal("full retired original view", err)
	}
	e.Views, err = storageEnrollmentPeakReadViews(viewsRaw, 1, e.Inner.Catalog, tickets)
	if err != nil {
		t.Fatal(err)
	}
	header := epBytes{}
	// Equal-generation byte-identical original host pages select bank0 under
	// both unchanged J2 and J3 selectors; the coverage reference follows it.
	for bank := byte(0); bank < 2; bank++ {
		h := epBytes{}
		h.fixed(uint16(0))
		h.fixed(bank)
		h.fixed(version)
		h.fixed(core.Generation)
		h.fixed(core.OperationID)
		h.fixed(sha256.Sum256(page))
		h.fixed(uint16(0))
		h.fixed(bank == 0)
		h.fixed(byte(storageIntentRetired))
		branch := byte(1)
		if later {
			branch = 2
		}
		h.fixed(branch)
		h.fixed(byte(0))
		b := make([]byte, 128)
		copy(b, h.Bytes())
		header.Write(b)
		header.Write(page)
	}
	e.Witnesses, err = storageEnrollmentPeakReadWitnesses(header.Bytes(), 2, e.Installation, e.QBinding)
	if err != nil {
		t.Fatal("retired raw witnesses", err)
	}
	origin := storageEnrollmentPeakOrigin{ID: storagebudget.ID{7}, Bridge: storagebudget.ID{7}, Epoch: 1, Revision: 10, View: 0, Projection: 0, Slots: 1, Witnesses: []uint16{0, 1}}
	o := epBytes{}
	o.fixed(origin.ID)
	o.fixed(origin.Bridge)
	o.fixed(origin.Epoch)
	o.fixed(origin.Revision)
	o.fixed(origin.View)
	o.fixed(origin.Projection)
	o.fixed(origin.Slots)
	o.fixed(uint16(2))
	o.fixed(uint16(0))
	o.fixed(uint16(1))
	origin.Digest = epHash("desk-enrollment-origin-record-e1", o.Bytes())
	e.Board.Origins = []storageEnrollmentPeakOrigin{origin}
	e.Board.Coverage = []storageEnrollmentPeakCoverage{{Slot: 0, Origin: 0, Witness: 0, Generation: core.Generation, Operation: core.OperationID, Page: sha256.Sum256(page), Epoch: e.CoveredEpoch, Bridge: e.Bridge, State: 1}}
	if _, err := storageEnrollmentPeakWriteBoard(e.Board, e.Scope, 1, e.Digests, e.Inner.Catalog, 1, 1, 2); err != nil {
		t.Fatal("retired board", err)
	}
	// Carry the actual persisted tuple and full original ticket into Q0, all
	// three predicted checkpoints and the complete evidence image, not a shadow
	// retirement bit. The original source ticket page oracle is independent.
	ticketBanks := epEmptyTickets(f.record)
	if !later {
		page := epTicketPage(v.Slots[slot].Ticket, f.record)
		for bank := 0; bank < 2; bank++ {
			copy(ticketBanks[bank][slot*16384:(slot+1)*16384], page)
		}
	}
	for i := 0; i < 4; i++ {
		copyQ := *f.q[i]
		a := v.Slots[slot]
		copyQ.Applied[slot] = storageEnrollmentPeakQApplied{ID: storageEnrollmentPeakQID(a.PersistedApplied.ID), Sequence: a.PersistedApplied.Sequence, Generation: a.PersistedApplied.Generation, NextSequence: a.PersistedNextSequence}
		f.q[i] = &copyQ
		f.cp[i] = epQBytes(&copyQ, f.record)
	}
	td, err := storageEnrollmentPeakWriteTickets(tickets)
	if err != nil {
		t.Fatal(err)
	}
	e.Sections[2].Raw = td
	e.Sections[2].Count = uint32(len(tickets))
	e.Sections[3].Raw = viewsRaw
	e.Sections[5].Raw = append([]byte{}, header.Bytes()...)
	e.Sections[5].Count = 2
	e.Sections[6].Raw = epCheckpoint(f.before, f.q[0], f.cp[0], ticketBanks)
	cpDigest := sha256.Sum256(f.cp[0])
	copy(e.Scope[120:152], cpDigest[:])
	viewDigest := sha256.Sum256(e.Views[0].Raw)
	copy(e.Scope[152:184], viewDigest[:])
	epScopeHash(&e.Scope)
	for i := 0; i < 3; i++ {
		e.Digests.Q[i] = sha256.Sum256(f.cp[i+1])
	}
	for i := range e.Board.Groups {
		switch e.Board.Groups[i].ID {
		case 21, 56:
			e.Board.Groups[i].Expected = e.Digests.Q[0]
		case 22, 57:
			e.Board.Groups[i].Expected = e.Digests.Q[1]
		case 23, 58:
			e.Board.Groups[i].Expected = e.Digests.Q[2]
		}
	}
	e.Sections[8].Raw, err = storageEnrollmentPeakWriteBoard(e.Board, e.Scope, 1, e.Digests, e.Inner.Catalog, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	epRelayout(&e)
	if 65536+e.Packed > e.Extent {
		t.Fatal("retired complete extent")
	}
	decoded, err := storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(e), f.record, f.claim)
	if err != nil {
		t.Fatal("complete retired evidence", err)
	}
	return decoded, core
}
func TestStorageEnrollmentPeakRetirementBranches(t *testing.T) {
	for _, identity := range []bool{false, true} {
		for _, later := range []bool{false, true} {
			t.Run(fmt.Sprintf("J%d-later-%t", 2+func() int {
				if identity {
					return 1
				}
				return 0
			}(), later), func(t *testing.T) {
				e, core := epRetired(t, identity, later)
				if !bytes.Equal(e.Witnesses[0].Page, e.Witnesses[1].Page) || !e.Witnesses[0].Selected || e.Witnesses[1].Selected || e.Board.Coverage[0].Witness != 0 {
					t.Fatal("identical original retired banks select bank0")
				}
				if !storageEnrollmentPeakProofs(&e) {
					t.Fatal("complete original retired branch")
				}
				_, record, claim := ownerAdvisoryFixture(false)
				// Reframe the coherent opposite selection with valid section/full
				// hashes. Original source selection still requires refusal.
				wrongBank := e
				wrongBank.Sections[5].Raw = append([]byte{}, e.Sections[5].Raw...)
				wrongBank.Sections[5].Raw[62] = 0
				wrongBank.Sections[5].Raw[16512+62] = 1
				wrongBank.Sections[8].Raw = append([]byte{}, e.Sections[8].Raw...)
				// Board prefix, one 98-byte origin (two refs), coverage count,
				// then coverage Slot/Origin precede SelectedWitnessRef.
				witnessRef := 174 + 24*int(e.Scope[326]) + len(e.Board.Groups)*115 + 2 + 98 + 2 + 2 + 2
				epSet16(wrongBank.Sections[8].Raw[witnessRef:], 1)
				wrongWitnesses, err := storageEnrollmentPeakReadWitnesses(wrongBank.Sections[5].Raw, 2, e.Installation, e.QBinding)
				if err != nil || wrongWitnesses[0].Selected || !wrongWitnesses[1].Selected {
					t.Fatal("structurally valid opposite witness selection", err)
				}
				wrongBoard, err := storageEnrollmentPeakReadBoard(wrongBank.Sections[8].Raw, e.Scope, e.Phase, e.Digests, e.Inner.Catalog, 1, 1, 2)
				if err != nil || wrongBoard.Coverage[0].Witness != 1 {
					t.Fatal("structurally valid opposite coverage selection", err)
				}
				epRelayout(&wrongBank)
				if _, err := storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(wrongBank), record, claim); err == nil {
					t.Fatal("complete retired opposite selected bank")
				}
				for _, mutation := range []string{"tuple", "next", "epoch", "root", "filesystem", "locator", "Q-binding", "ticket", "witness-ref", "selected-bank"} {
					bad := e
					bad.Views = append([]storageEnrollmentPeakView{}, e.Views...)
					bad.Board.Coverage = append([]storageEnrollmentPeakCoverage{}, e.Board.Coverage...)
					bad.Witnesses = append([]storageEnrollmentPeakWitness{}, e.Witnesses...)
					slot := int(core.Token.Slot)
					switch mutation {
					case "tuple":
						bad.Views[0].Slots[slot].PersistedApplied.ID = storagebudget.ID{}
					case "next":
						bad.Views[0].Slots[slot].PersistedNextSequence = core.Token.Sequence
					case "epoch":
						if !later && !identity {
							continue
						}
						bad.Views[0].Epoch++
					case "root":
						bad.Witnesses[0].Core.RootIdentities = append([]storagebudget.Digest{}, core.RootIdentities...)
						bad.Witnesses[0].Core.RootIdentities[0][0] ^= 1
					case "filesystem":
						bad.Witnesses[0].Core.FilesystemIdentities = append([]storagebudget.Digest{}, core.FilesystemIdentities...)
						bad.Witnesses[0].Core.FilesystemIdentities[0][0] ^= 1
					case "locator":
						bad.Witnesses[0].Core.Locators = []string{"changed"}
					case "Q-binding":
						if later || !identity {
							continue
						}
						bad.QBinding[0] ^= 1
					case "ticket":
						if later || !identity {
							continue
						}
						bad.Views[0].Slots[slot].Ticket.Evidence[0] ^= 1
					case "witness-ref":
						bad.Board.Coverage[0].Witness = 1
					case "selected-bank":
						bad.Witnesses[0].Selected = false
						bad.Witnesses[1].Selected = true
						bad.Board.Coverage[0].Witness = 1
					}
					if storageEnrollmentPeakProofs(&bad) {
						t.Fatal("retired refusal", mutation)
					}
				}
			})
		}
	}
}

func TestStorageEnrollmentPeakKind2CompleteSource(t *testing.T) {
	c, r, cl := ownerAdvisoryFixture(false)
	ownerCatalogHome(&c)
	c.Workspaces = append(c.Workspaces, storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: storagebudget.ID{4}, Generation: 1, Limits: c.Policy.Workspace}, OwnerKind: 1, OwnerKey: []byte(strings.Repeat("f", 64)), HistoryIdentity: []byte("/project")})
	ownerCatalogSeal(&c, &r, &cl)
	if _, err := storageOwnerDecodeCatalogV3(ownerAdvisoryWire(c), r, cl); err != nil {
		t.Fatal("original home fixture", err)
	}
	workspace := c.Workspaces[len(c.Workspaces)-1]
	b := storageEnrollmentPeakSource{Installation: c.Installation, QBinding: c.QBinding, Project: epNative(100, 1), Workspace: workspace.Definition.ID, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: []byte("selected-project-owner"), HistoryIdentity: workspace.HistoryIdentity, Filesystem: c.Filesystems[0].Definition.ID, FilesystemIdentity: c.Filesystems[0].Definition.Identity, FilesystemGeneration: 1, CapacityCount: c.Roots[0].Definition.CapacityCount, CapacityRefs: c.Roots[0].Definition.CapacityRefs, Namespace: []byte(".desk-private"), Generation: 1}
	selected := -1
	for i, h := range c.OriginalOwner.Entries {
		if h.RunnerWorkspace == string(workspace.OwnerKey) && bytes.Equal(h.HistoryIdentity, workspace.HistoryIdentity) {
			selected = i
		}
	}
	if selected < 0 {
		t.Fatal("selected original home entry")
	}
	registry := c.OwnerControls[c.OriginalOwner.RegistryControl]
	registryRaw := fmt.Sprintf(`{"Role":%d,"Parent":%s,"Name":%s,"OwnerKind":%d,"OwnerKey":%s,"Location":%s,"RelationDigest":%s,"MaxBytes":%d}`, registry.Role, epJSON(c.Objects[registry.ParentObject].Persistent), epJSON(registry.Name), registry.OwnerKind, epJSON(registry.OwnerKey), epJSON(registry.Location), epJSON(registry.RelationDigest), registry.MaxBytes)
	material := fmt.Sprintf(`{"Installation":%s,"QBinding":%s,"Project":%s,"Workspace":%s,"OwnerKey":%s,"HistoryIdentity":%s,"Kind":2,"ConfigPath":%s,"HomeOwner":%s,"HomePath":%s,"HomeObject":%s,"Registry":%s,"Entry":%s}`, epJSON(b.Installation), epJSON(b.QBinding), epJSON(b.Project), epJSON(workspace), epJSON(b.OwnerKey), epJSON(b.HistoryIdentity), epJSON(c.OriginalOwner.ConfigPath), epJSON(c.OriginalOwner.HomeOwner), epJSON(c.OriginalOwner.HomePath), epJSON(c.OriginalOwner.HomeObject), registryRaw, epJSON(c.OriginalOwner.Entries[selected]))
	b.HomeSource = epHash("desk-project-private-home-source-peak-v1", []byte(material))
	if !storageEnrollmentPeakHomeSourceMatches(b, c) {
		t.Fatal("complete Kind2 source selection")
	}
	for _, mutation := range []string{"registry-name", "registry-parent", "registry-relation", "entry-history", "entry-runner", "entry-folder", "workspace", "home-native", "home-path", "project", "owner", "history"} {
		bad := ownerCatalogClone(c)
		bb := b
		switch mutation {
		case "registry-name":
			bad.OwnerControls[bad.OriginalOwner.RegistryControl].Name = []byte("other.json")
		case "registry-parent":
			bad.OwnerControls[bad.OriginalOwner.RegistryControl].ParentObject = 1
		case "registry-relation":
			bad.OwnerControls[bad.OriginalOwner.RegistryControl].RelationDigest[0] ^= 1
		case "entry-history":
			bad.OriginalOwner.Entries[selected].HistoryIdentity = []byte("changed")
		case "entry-runner":
			bad.OriginalOwner.Entries[selected].RunnerWorkspace = "changed"
		case "entry-folder":
			bad.OriginalOwner.Entries[selected].Folder = []byte("other")
		case "workspace":
			bad.Workspaces[len(bad.Workspaces)-1].Definition.Generation++
		case "home-native":
			bad.OriginalOwner.HomeObject.Object++
		case "home-path":
			bad.OriginalOwner.HomePath = []byte("/other")
		case "project":
			bb.Project.Object++
		case "owner":
			bb.OwnerKey = []byte("other")
		case "history":
			bb.HistoryIdentity = []byte("other")
		}
		if storageEnrollmentPeakHomeSourceMatches(bb, bad) {
			t.Fatal("selected full source mutation", mutation)
		}
	}
}

func TestStorageEnrollmentPeakExactThresholdsAndSelectors(t *testing.T) {
	for _, n := range []int{8384128, 8384129} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c, r, cl := epV3Size(t, n)
			old := ownerAdvisoryWire(c)
			if _, err := storageOwnerDecodeCatalogV3(old, r, cl); err != nil {
				t.Fatal("threshold original-valid", err)
			}
			s := epScope(1, false, r, [2]uint64{}, [2]uint64{65536, 65536})
			epSet64(s[1624:], c.Sequence)
			d := sha256.Sum256(old)
			copy(s[88:120], d[:])
			epScopeHash(&s)
			snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: d, Inner: epHash("desk-enrollment-original-v3-inner-peak-v1", epJSON(c)), Before: old}
			raw, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: c.Sequence + 1, Scope: s, Snapshot: &snapshot}, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			want := byte(0)
			if n == 8384129 {
				want = 1
			}
			if raw[66] != want {
				t.Fatal("exact raw iff threshold", n, raw[66])
			}
			decoded, err := storageEnrollmentPeakDecodeEnvelope(raw, r, cl, true)
			if err != nil || !bytes.Equal(decoded.Snapshot.Before, old) {
				t.Fatal("threshold full original state", err)
			}
			projection := epProjection(c, c, old)
			restored, err := storageEnrollmentPeakReadProjection(projection, c, old, r, cl)
			if err != nil || !reflect.DeepEqual(restored.Catalog, c) || len(projection) > 135168 {
				t.Fatal("complete near-maximum source projection", len(projection), err)
			}
		})
	}
	_, r, _ := ownerAdvisoryFixture(false)
	count := 0
	for path := byte(1); path <= 3; path++ {
		for c := byte(0); c < 2; c++ {
			for q := byte(0); q < 2; q++ {
				for e := byte(0); e < 2; e++ {
					for d := byte(0); d < 2; d++ {
						if path == 1 && (e != 0 || d != 0) {
							continue
						}
						before := [2]uint64{}
						final := [2]uint64{71303168, 71303168}
						if path != 1 {
							before = [2]uint64{65536, 65536}
							if path == 2 {
								before = final
							}
						}
						s := epScope(path, true, r, before, final)
						s[322] = c
						s[325] = q
						if path != 1 {
							s[323] = e
							s[324] = d
						}
						epReplan(&s)
						if _, err := storageEnrollmentPeakDecodeScope(s[:]); err != nil {
							t.Fatal("selector matrix", path, c, e, d, q, err)
						}
						got := storageEnrollmentPeakPlan(s)
						want := epGroups(path, final, c, s[323], s[324], q)
						if len(got) != len(want) {
							t.Fatal("selector length")
						}
						for i, g := range got {
							w := want[i]
							w.Generation = 0
							w.Native = s.target(w.Target)
							if g != w {
								t.Fatal("selector row", path, c, e, d, q, g.ID)
							}
						}
						count++
					}
				}
			}
		}
	}
	if count != 36 {
		t.Fatal("all feasible selector field combinations", count)
	}
}

func TestStorageEnrollmentPeakFullViewAndProjectionBounds(t *testing.T) {
	c, r, cl := ownerAdvisoryFixture(false)
	tickets := []storagebudget.Ticket{}
	for generation := uint64(0); generation < 66; generation++ {
		for slot := byte(0); slot < 64; slot++ {
			tickets = append(tickets, storagebudget.Ticket{Token: storagebudget.Token{Slot: slot, Generation: generation}, Components: []storagebudget.Component{}, Changes: []storagebudget.Change{}})
		}
	}
	sort.Slice(tickets, func(i, j int) bool { return bytes.Compare(epTicketBody(tickets[i]), epTicketBody(tickets[j])) < 0 })
	views := []storageEnrollmentPeakView{}
	expected := epBytes{}
	for i := 0; i < 66; i++ {
		v, _ := epView(c, epQ0(c))
		v.Revision = uint64(10 + i)
		expected.fixed(v.Revision)
		expected.fixed(v.Epoch)
		expected.fixed(v.Maintenance)
		expected.fixed(v.Policy)
		for _, refs := range v.DefinitionRefs {
			expected.fixed(uint16(len(refs)))
			for _, ref := range refs {
				expected.fixed(ref)
			}
		}
		for slot := 0; slot < 64; slot++ {
			v.Slots[slot].Ticket.Token.Generation = uint64(i)
			expected.fixed(uint16(slot*66 + i))
			expected.fixed(v.Slots[slot].PersistedApplied)
			expected.fixed(v.Slots[slot].PersistedNextSequence)
		}
		views = append(views, v)
	}
	raw, err := storageEnrollmentPeakWriteViews(views, c, tickets)
	if err != nil || !bytes.Equal(raw, expected.Bytes()) {
		t.Fatal("66 complete views /4224 exact typed ticket refs", err)
	}
	if decoded, err := storageEnrollmentPeakReadViews(raw, 66, c, tickets); err != nil || len(decoded) != 66 {
		t.Fatal("max complete views", err)
	}
	for _, bad := range []struct {
		raw []byte
		n   uint32
	}{{raw, 67}, {raw[:len(raw)-1], 66}, {append(append([]byte{}, raw...), 0), 66}} {
		if _, err := storageEnrollmentPeakReadViews(bad.raw, bad.n, c, tickets); err == nil {
			t.Fatal("view count/length bound")
		}
	}
	// Each of the66 projections is a complete, distinct, original-valid v3
	// image sharing only equal immutable dictionary rows; annotations stay exact.
	old := ownerAdvisoryWire(c)
	records := [][]byte{}
	for i := 0; i < 66; i++ {
		v := ownerCatalogClone(c)
		if i > 0 {
			v.Sequence = uint64(i + 1)
			v.PreviousCatalogDigest = storagebudget.Digest{1}
			v.Transition = storagebudget.ID{byte(i + 20)}
		}
		outer := ownerAdvisoryWire(v)
		p := epProjection(v, c, outer)
		if i > 0 {
			epSet16(p[len(p)-68:], 65535)
		}
		if _, err := storageEnrollmentPeakReadProjection(p, c, old, r, cl); err != nil {
			t.Fatal("complete projection", i, err)
		}
		records = append(records, p)
	}
	sort.Slice(records, func(i, j int) bool { return bytes.Compare(records[i], records[j]) < 0 })
	for i := 1; i < len(records); i++ {
		if bytes.Equal(records[i-1], records[i]) {
			t.Fatal("complete-record interning")
		}
	}
	// The maximum counts are conservative independent caps, not permission to
	// fabricate a catalog exceeding its original8MiB/source-byte bounds.
	referenceBytes := 2 * (8192 + 0 + 4096 + 1024 + 128 + 512 + 64 + 1024)
	beforeIDBytes := 16 * (4096 + 1024 + 128 + 512)
	fixed := 8 + 4 + 16 + 32 + 8 + 2 + 8 + 16 + 32 + 32 + 32 + 8 + 488 + 2 + 4 + 8*2 + 4*2 + 2 + 2 + 8320
	if referenceBytes+beforeIDBytes+fixed != 131280 || 131280 > 135168 || 66*135168 != 8921088 {
		t.Fatal("exact complete projection width proof")
	}
	if _, err := storageEnrollmentPeakReadProjection(make([]byte, 135169), c, old, r, cl); err == nil {
		t.Fatal("projection concrete cap")
	}
	// Decode66 complete records in the full64-origin frame as well. The exact
	// original v3 projection stays numbered0 after lexicographic ordering.
	full, _, f := epOriginFixture(t)
	for i := range records {
		v := ownerCatalogClone(f.before)
		if i > 0 {
			v.Sequence = uint64(i + 1)
			v.PreviousCatalogDigest = storagebudget.Digest{1}
			v.Transition = storagebudget.ID{byte(i + 20)}
		}
		outer := ownerAdvisoryWire(v)
		records[i] = epProjection(v, full.Inner.Catalog, outer)
		if i > 0 {
			epSet16(records[i][len(records[i])-68:], 65535)
		}
	}
	sort.Slice(records, func(i, j int) bool { return bytes.Compare(records[i], records[j]) < 0 })
	joined := bytes.Join(records, nil)
	full.Sections[4].Count = 66
	full.Sections[4].Raw = joined
	epRelayout(&full)
	if 65536+full.Packed > full.Extent {
		t.Fatal("actual66 projections fit existing planned extent")
	}
	if _, err := storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(full), f.record, f.claim); err != nil {
		t.Fatal("full64-origin/66-projection evidence", err)
	}
}

func TestStorageEnrollmentPeakMaximumBoardCombination(t *testing.T) {
	full, _, f := epOriginFixture(t)
	before, after, b, r, cl := epCatalogs(t)
	history := []byte("/" + strings.Repeat("h", 4095))
	before.Workspaces[0].HistoryIdentity = history
	ownerCatalogSeal(&before, &r, &cl)
	after.Workspaces[0].HistoryIdentity = history
	parent := after.Roots[7]
	b.OwnerKey = bytes.Repeat([]byte{'o'}, 4096)
	b.HistoryIdentity = history
	b.HomeSource = epHomeSource(b, after)
	b.CapacityBinding = epMapping(b, after, parent)
	parent.Source.OwnerKey = append([]byte{}, b.OwnerKey...)
	parent.Source.Relative = epSourceBytes(b)
	parent.Definition.Identity = epHash("desk-project-private-parent-scope-e1", parent.Source.Relative)
	d := parent.Definition
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range after.Constraints {
		constraints = append(constraints, v.Definition)
	}
	var err error
	parent.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, after.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	after.Roots[7] = parent
	old := ownerAdvisoryWire(before)
	copy(after.PreviousCatalogDigest[:], old[32:64])
	epSealDefinitions(&after)
	if err := storageEnrollmentPeakValidateCatalog(after, r, cl); err != nil {
		t.Fatal("maximum complete source keys", err)
	}
	q1 := *f.q[1]
	q2 := q1
	q2.Generation++
	epInto(&q2.Roots[7], parent.Definition)
	q3 := q2
	q3.Generation++
	epMeasuredQ(&q3)
	qd := [3]storagebudget.Digest{sha256.Sum256(epQBytes(&q1, r)), sha256.Sum256(epQBytes(&q2, r)), sha256.Sum256(epQBytes(&q3, r))}
	registration := storagebudget.Registration{Roots: []storagebudget.Root{}, Workspaces: []storagebudget.Workspace{}, Filesystems: []storagebudget.Filesystem{}, CapacityConstraints: []storagebudget.CapacityConstraint{}}
	var root storagebudget.Root
	epInto(&root, parent.Definition)
	registration.Roots = append(registration.Roots, root)
	o := epBytes{}
	o.fixed(after.Installation)
	o.fixed(after.QBinding)
	o.fixed(after.Transition)
	o.fixed(b.Project)
	o.fixed(sha256.Sum256(epJSON(registration)))
	origin := epHash("desk-enrollment-origin-e1", o.Bytes())
	s := epScope(1, true, r, [2]uint64{}, [2]uint64{71303168, 71303168})
	copy(s[440:456], after.Constraints[0].Definition.ID[:])
	epReplan(&s)
	base := epBoard(s, b, 1, qd, origin)
	newGroupEnd := 174 + 24*int(s[326]) + 62*115
	oldGroupEnd := 174 + 24*int(full.Scope[326]) + len(full.Board.Groups)*115
	tablesLength := 2 + 64*(94+4) + 2 + 64*87
	tables := full.Sections[8].Raw[oldGroupEnd : oldGroupEnd+tablesLength]
	tailEnd := newGroupEnd + 4 + 96 + 10 + 1 + 8543
	raw := append([]byte{}, base[:newGroupEnd]...)
	raw = append(raw, tables...)
	raw = append(raw, base[newGroupEnd+4:tailEnd]...)
	if len(raw) != 27822 {
		t.Fatal("complete62-group/64-origin/128-witness/8543-byte-source board used bytes", len(raw))
	}
	raw = append(raw, make([]byte, 65536-len(raw))...)
	board, err := storageEnrollmentPeakReadBoard(raw, s, 1, storageEnrollmentPeakBoardDigests{Q: qd, Origin: origin}, after, 1, 1, 128)
	if err != nil || len(board.Groups) != 62 || len(board.Origins) != 64 || len(board.Coverage) != 64 {
		t.Fatal("maximum complete board combination", err)
	}
	if encoded, err := storageEnrollmentPeakWriteBoard(board, s, 1, storageEnrollmentPeakBoardDigests{Q: qd, Origin: origin}, after, 1, 1, 128); err != nil || !bytes.Equal(encoded, raw) {
		t.Fatal("maximum handwritten board image", err)
	}
	for _, offset := range []int{newGroupEnd, newGroupEnd + 2 + 64*98, newGroupEnd + tablesLength + 96, newGroupEnd + tablesLength + 96 + 10 + 1 + 107} {
		bad := append([]byte{}, raw...)
		epSet16(bad[offset:], 65535)
		if _, err := storageEnrollmentPeakReadBoard(bad, s, 1, storageEnrollmentPeakBoardDigests{Q: qd, Origin: origin}, after, 1, 1, 128); err == nil {
			t.Fatal("maximum row/source/count refusal", offset)
		}
	}
}

func TestStorageEnrollmentPeakCompleteRegistrationMaterial(t *testing.T) {
	// Handwritten complete comparison material, not a Q registration grant. The
	// literal pins every nested original DTO member and default JSON escaping.
	const raw = `{"Roots":[{"ID":[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Owner":[2,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Identity":[3,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Filesystem":[4,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Workspace":[5,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":5,"Locator":"\u003c\u003e\u0026\u2028\u2029","Known":false,"Usage":[{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0}],"CapacityCount":1,"CapacityRefs":[{"Constraint":[6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":7},{"Constraint":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":0},{"Constraint":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":0},{"Constraint":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":0}],"CapacityBinding":[8,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}],"Workspaces":[{"ID":[5,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":2,"Known":false,"Usage":[{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0},{"Bytes":0,"Files":0,"Directories":0}],"Limits":{"Total":{"Bytes":9,"Files":8,"Directories":7},"Category":[{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7},{"Bytes":9,"Files":8,"Directories":7}]}}],"Filesystems":[{"ID":[4,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Identity":[9,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":3,"InodeModel":1,"CapabilityEvidence":[10,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"ByteConstraint":[6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"InodeConstraint":[6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]}],"CapacityConstraints":[{"ID":[6,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Identity":[11,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Generation":7,"Resources":3,"Headroom":{"Bytes":12,"Inodes":13}}]}`
	const want = "0e0729be165feb2e4283c1b3fc3035da59ae493010c1d6eeb0ab36c9137878ea"
	var registration storagebudget.Registration
	if err := json.Unmarshal([]byte(raw), &registration); err != nil {
		t.Fatal(err)
	}
	if string(epJSON(registration)) != raw {
		t.Fatal("complete original declarations/order/arrays/escaping")
	}
	d, err := storageEnrollmentPeakRegistrationDigest(registration)
	if err != nil || hex.EncodeToString(d[:]) != want {
		t.Fatal("complete literal hash", err)
	}
	type step struct {
		field bool
		index int
	}
	paths := [][]step{}
	var visit func(reflect.Value, []step)
	visit = func(v reflect.Value, path []step) {
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				visit(v.Field(i), append(append([]step{}, path...), step{true, i}))
			}
		case reflect.Array, reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				visit(v.Index(i), append(append([]step{}, path...), step{false, i}))
			}
		default:
			paths = append(paths, append([]step{}, path...))
		}
	}
	visit(reflect.ValueOf(registration), nil)
	for _, path := range paths {
		var mutated storagebudget.Registration
		epInto(&mutated, registration)
		v := reflect.ValueOf(&mutated).Elem()
		for _, s := range path {
			if s.field {
				v = v.Field(s.index)
			} else {
				v = v.Index(s.index)
			}
		}
		switch v.Kind() {
		case reflect.Uint8, reflect.Uint64:
			v.SetUint(v.Uint() + 1)
		case reflect.Bool:
			v.SetBool(!v.Bool())
		case reflect.String:
			v.SetString(v.String() + "changed")
		default:
			t.Fatal("uncovered nested original field", v.Kind())
		}
		got, err := storageEnrollmentPeakRegistrationDigest(mutated)
		if err != nil || got == d {
			t.Fatal("nested field absent from material", path, err)
		}
	}
	for _, field := range []string{"Roots", "Workspaces", "Filesystems", "CapacityConstraints"} {
		var mutated storagebudget.Registration
		epInto(&mutated, registration)
		reflect.ValueOf(&mutated).Elem().FieldByName(field).SetZero()
		if _, err := storageEnrollmentPeakRegistrationDigest(mutated); err == nil {
			t.Fatal("nil array repaired", field)
		}
	}
	ordered := registration
	ordered.Roots = append(append([]storagebudget.Root{}, registration.Roots...), registration.Roots[0])
	ordered.Roots[1].ID[0] = 50
	a, _ := storageEnrollmentPeakRegistrationDigest(ordered)
	ordered.Roots[0], ordered.Roots[1] = ordered.Roots[1], ordered.Roots[0]
	z, _ := storageEnrollmentPeakRegistrationDigest(ordered)
	if a == z {
		t.Fatal("original array order omitted")
	}
	if len(paths) != 484 {
		t.Fatal("complete nested scalar coverage", len(paths))
	}
}

func epTicketPage(t storagebudget.Ticket, record storageOwnerRecordV2) []byte {
	raw := make([]byte, 16384)
	w := epBytes{}
	w.fixed(t.Token.ID)
	w.fixed(t.Token.Sequence)
	w.fixed(byte(t.State))
	w.fixed(t.PolicyRevision)
	for _, text := range []string{t.Workflow, t.Operation} {
		w.fixed(uint16(len(text)))
		field := make([]byte, 128)
		copy(field, text)
		w.Write(field)
	}
	w.fixed(t.Owner)
	w.fixed(t.Manifest)
	w.fixed(t.ManifestLength)
	w.fixed(t.Evidence)
	w.fixed(byte(len(t.Components)))
	w.fixed(byte(len(t.Changes)))
	copy(raw[128:], w.Bytes())
	for i, c := range t.Components {
		w.Reset()
		w.fixed(c.Root)
		w.fixed(c.RootGeneration)
		w.fixed(c.Owner)
		w.fixed(c.Filesystem)
		w.fixed(c.FilesystemGeneration)
		w.fixed(c.Workspace)
		w.fixed(c.WorkspaceGeneration)
		w.fixed(c.Category)
		w.fixed(c.Physical)
		w.fixed(c.Attribution)
		w.fixed(c.SettlementPhysical)
		w.fixed(c.SettlementAttribution)
		if i < len(t.Changes) {
			w.fixed(t.Changes[i])
		} else {
			w.Write(make([]byte, 96))
		}
		w.fixed(c.CapacityCount)
		w.fixed(c.Capacity)
		w.fixed(c.CapacityBinding)
		copy(raw[1024+i*512:], w.Bytes())
	}
	epQSeal(raw, 2, uint16(t.Token.Slot), t.Token.Generation, record)
	return raw
}

// Both original ticket banks, Q0/all predicted persisted tuples, typed view,
// section digests and complete frame agree. A foreign workflow is intentional.
func epForeignTicket(t *testing.T, terminal bool) (epComplete, storagebudget.Ticket) {
	t.Helper()
	f := epEvidence(t, 1)
	e := epDecode(t, f)
	d := f.before.Roots[0].Definition
	fs := f.before.Filesystems[0].Definition
	component := storagebudget.Component{Root: d.ID, RootGeneration: d.Generation, Owner: d.Owner, Filesystem: d.Filesystem, FilesystemGeneration: fs.Generation, Category: 4, Physical: storagebudget.Vector{Bytes: 512, Files: 1}, SettlementPhysical: storagebudget.Vector{Bytes: 134217728}, CapacityCount: d.CapacityCount, CapacityBinding: d.CapacityBinding}
	for i := 0; i < int(d.CapacityCount); i++ {
		component.Capacity[i] = storagebudget.CapacityPromise{Constraint: d.CapacityRefs[i].Constraint, Generation: d.CapacityRefs[i].Generation, Peak: storagebudget.CapacityVector{Bytes: 135266304, Inodes: 1}}
	}
	ticket := storagebudget.Ticket{Token: storagebudget.Token{Slot: 1, ID: storagebudget.ID{93}, Sequence: 1, Generation: 1}, State: storagebudget.Reserved, PolicyRevision: 1, Workflow: "foreign-workflow", Operation: "foreign-operation", Owner: f.record.QBinding, Components: []storagebudget.Component{component}, Changes: []storagebudget.Change{}}
	view := e.Views[0]
	if terminal {
		ticket.Token.Generation = 3
		ticket.State = storagebudget.Terminal
		ticket.Evidence = storagebudget.Digest{3}
		ticket.Changes = []storagebudget.Change{{}}
		view.Slots[1].PersistedApplied = ticket.Token
		view.Slots[1].PersistedNextSequence = 2
	}
	view.Slots[1].Ticket = ticket
	qt := storageEnrollmentPeakQTicketValue(ticket)
	if storageEnrollmentPeakQValidateTicket(qt) != nil || storageEnrollmentPeakQRegistered(f.q[0], qt, false) != nil {
		t.Fatal("foreign ticket has complete registered original material")
	}
	page := epTicketPage(ticket, f.record)
	for bank := 0; bank < 2; bank++ {
		copy(f.tickets[bank][16384:32768], page)
	}
	tickets := append(append([]storagebudget.Ticket{}, e.Tickets...), ticket)
	sort.Slice(tickets, func(i, j int) bool { return bytes.Compare(epTicketBody(tickets[i]), epTicketBody(tickets[j])) < 0 })
	var err error
	f.sections[2], err = storageEnrollmentPeakWriteTickets(tickets)
	if err != nil {
		t.Fatal(err)
	}
	f.sections[3], err = storageEnrollmentPeakWriteViews([]storageEnrollmentPeakView{view}, f.after, tickets)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		q := *f.q[i]
		a := view.Slots[1]
		q.Applied[1] = storageEnrollmentPeakQApplied{ID: storageEnrollmentPeakQID(a.PersistedApplied.ID), Sequence: a.PersistedApplied.Sequence, Generation: a.PersistedApplied.Generation, NextSequence: a.PersistedNextSequence}
		f.q[i] = &q
		f.cp[i] = epQBytes(&q, f.record)
	}
	f.sections[6] = epCheckpoint(f.before, f.q[0], f.cp[0], f.tickets)
	hash := sha256.Sum256(f.cp[0])
	copy(f.scope[120:152], hash[:])
	hash = sha256.Sum256(f.sections[3])
	copy(f.scope[152:184], hash[:])
	epScopeHash(&f.scope)
	qd := [3]storagebudget.Digest{sha256.Sum256(f.cp[1]), sha256.Sum256(f.cp[2]), sha256.Sum256(f.cp[3])}
	f.sections[8] = epBoard(f.scope, f.source, 1, qd, e.Digests.Origin)
	e.Scope = f.scope
	for i := range e.Sections {
		e.Sections[i].Raw = f.sections[i]
	}
	e.Sections[2].Count = 65
	epRelayout(&e)
	f.evidence = e
	f.raw = epEvidenceBytes(e)
	return f, ticket
}

func epPrefixComparison(t *testing.T, f epComplete) storageEnrollmentPeakComparisonInput {
	t.Helper()
	e := f.evidence
	scope := f.scope
	scope[321] = 0
	clear(scope[1080:1220])
	old := ownerAdvisoryWire(f.before)
	snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: sha256.Sum256(old), Inner: epHash("desk-enrollment-original-v3-inner-peak-v1", epJSON(f.before)), Before: old}
	fence, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: 2, Scope: scope, Snapshot: &snapshot}, f.record, f.claim)
	if err != nil {
		t.Fatal(err)
	}
	original := epOriginal(f, e)
	for i := 0; i < 4; i++ {
		original.Targets[i] = storageOwnerPersistentObject{}
		original.Extents[i] = 0
	}
	return storageEnrollmentPeakComparisonInput{Catalogs: [2][]byte{fence, old}, Prefix: storageEnrollmentPeakPrefix{Catalog: epJSON(f.after), Projection: f.sections[4], Tickets: f.sections[2], Views: f.sections[3], Checkpoint: f.sections[6], TicketCount: e.Sections[2].Count, ViewCount: 1}, Original: original, CurrentCheckpoints: original.Checkpoints, CurrentTickets: original.Tickets}
}

func TestStorageEnrollmentPeakOriginalCensusEntryPredicates(t *testing.T) {
	f := epEvidence(t, 1)
	e := epDecode(t, f)
	if e.Predictions[2].Installation[4] != (storageEnrollmentPeakQVector{67108864, 16, 1}) || e.Census.Roots[0].Usage[4] != (storageEnrollmentPeakQVector{67108864, 16, 1}) {
		t.Fatal("real F and independent Q3 meet exact original Control floor")
	}
	staged := epDecode(t, epEvidence(t, 0))
	if staged.Census.Evidence != (storageEnrollmentPeakQDigest{}) || staged.Digests.Q[2] != (storagebudget.Digest{}) || staged.Census.Roots[0].Usage != ([9]storageEnrollmentPeakQVector{}) {
		t.Fatal("unarmed placeholder remains unavailable")
	}
	for component := 0; component < 3; component++ {
		t.Run(fmt.Sprintf("below-Control-%d", component), func(t *testing.T) {
			bad := e
			bad.Sections[7].Raw = append([]byte{}, e.Sections[7].Raw...)
			offset := 32 + 2 + 16 + 8 + 4*24 + component*8
			value := binary.LittleEndian.Uint64(bad.Sections[7].Raw[offset:])
			epSet64(bad.Sections[7].Raw[offset:], value-1)
			bad.CensusDigest = sha256.Sum256(bad.Sections[7].Raw)
			census, err := storageEnrollmentPeakReadCensus(bad.Sections[7].Raw, e.Predictions[1], 1)
			if err != nil {
				t.Fatal("structurally valid real F", err)
			}
			projected := *e.Predictions[1]
			projected.Generation++
			projected.Maintenance = false
			if storageEnrollmentPeakQPublishCensus(&projected, census) != nil || storageEnrollmentPeakQValidateCheckpoint(&projected) != nil {
				t.Fatal("original low-level projection alone lacks public floor")
			}
			want := *f.q[3]
			vector := &want.Installation[4]
			switch component {
			case 0:
				vector.Bytes--
			case 1:
				vector.Files--
			case 2:
				vector.Directories--
			}
			want.Roots[0].Usage[4] = *vector
			if !bytes.Equal(epQBytes(&projected, f.record), epQBytes(&want, f.record)) {
				t.Fatal("independent below-floor Q3 bytes")
			}
			qd := e.Digests.Q
			qd[2] = sha256.Sum256(epQBytes(&want, f.record))
			bad.Sections[8].Raw = epBoard(e.Scope, f.source, 1, qd, e.Digests.Origin)
			epRelayout(&bad)
			if _, err := storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(bad), f.record, f.claim); err == nil {
				t.Fatal("accepted below original Control floor", component)
			}
		})
	}
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("foreign-terminal-%t", terminal), func(t *testing.T) {
			candidate, ticket := epForeignTicket(t, terminal)
			prefix := epPrefixComparison(t, candidate)
			_, prefixErr := storageEnrollmentPeakCompare(prefix, candidate.record, candidate.claim)
			decoded, decodeErr := storageEnrollmentPeakDecodeEvidence(candidate.raw, candidate.record, candidate.claim)
			if terminal {
				if prefixErr != nil || decodeErr != nil {
					t.Fatal("valid applied foreign terminal twin", prefixErr, decodeErr)
				}
				if !reflect.DeepEqual(decoded.Views[0].Slots[1].Ticket, ticket) {
					t.Fatal("full terminal material")
				}
			} else if prefixErr == nil || decodeErr == nil {
				t.Fatal("coherent active foreign ticket admitted", prefixErr, decodeErr)
			}
			anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(candidate.record.Installation), Binding: storageEnrollmentPeakQDigest(candidate.record.QBinding)}
			for bank := 0; bank < 2; bank++ {
				if _, err := storageEnrollmentPeakQDecodeCheckpoint(candidate.cp[0], anchor); err != nil {
					t.Fatal("actual original Q0", err)
				}
				got, err := storageEnrollmentPeakQDecodeTicket(candidate.tickets[bank][16384:32768], 1, anchor)
				if err != nil || got.State != storageEnrollmentPeakQState(ticket.State) {
					t.Fatal("actual coherent original ticket banks", err)
				}
			}
			context := candidate.evidence
			context.Checkpoint.Checkpoint = candidate.cp[0]
			for bank := 0; bank < 2; bank++ {
				context.Checkpoint.Checkpoints[bank] = sha256.Sum256(candidate.cp[0])
				for slot := 0; slot < 64; slot++ {
					context.Checkpoint.Tickets[slot][bank] = sha256.Sum256(candidate.tickets[bank][slot*16384 : (slot+1)*16384])
				}
			}
			for i := range context.PredictionBytes {
				context.PredictionBytes[i] = candidate.cp[i+1]
			}
			result, qErr := storageEnrollmentPeakCompareQ(context, [2][]byte{candidate.cp[0], candidate.cp[0]}, candidate.tickets, storageEnrollmentPeakDispatch{Bridge: context.Bridge, Step: 0})
			if terminal && (qErr != nil || result.Stage != 0) || !terminal && qErr == nil {
				t.Fatal("all-workflow actual-Q entry predicate", terminal, qErr)
			}
		})
	}
}

func TestStorageEnrollmentPeakUnequalEvidenceExtents(t *testing.T) {
	for _, path := range []byte{2, 3} {
		t.Run(fmt.Sprint(path), func(t *testing.T) {
			armed, prior, oldOuter := epExistingExtents(t, path, 1, true)
			completed, _, _ := epExistingExtents(t, path, 2, true)
			a, c := epDecode(t, armed), epDecode(t, completed)
			if a.Extent == c.Extent || a.Packed != c.Packed || !storageEnrollmentPeakJoinArmed(c, a) {
				t.Fatal("unequal exact per-file extents preserve packed/F join")
			}
			wantA, wantC := byte(0), byte(1)
			if path == 3 {
				wantA, wantC = 1, 0
			}
			if a.Bank != wantA || c.Bank != wantC {
				t.Fatal("handwritten no-growth/growth banks", a.Bank, c.Bank)
			}
			definitions := epNormal(t, a, 2, func() uint64 {
				if path == 2 {
					return 9
				}
				return 10
			}(), storagebudget.Digest{1}, armed.record, armed.claim)
			previous := storagebudget.Digest{}
			copy(previous[:], definitions[32:64])
			closed := epNormal(t, c, 3, func() uint64 {
				if path == 2 {
					return 10
				}
				return 11
			}(), previous, armed.record, armed.claim)
			pBank := 0
			if path == 3 {
				pBank = 1
			}
			pair := [2][]byte{definitions, definitions}
			pair[pBank] = closed
			oldDispatch := epPeerDispatch(epDecode(t, prior))[1]
			entered := epDispatch(a, 0, 5, sha256.Sum256(oldDispatch))
			begin := epDispatch(a, 1, 6, sha256.Sum256(entered))
			register := epDispatch(a, 2, 7, sha256.Sum256(begin))
			census := epDispatch(a, 3, 8, sha256.Sum256(register))
			original := epOriginal(armed, a)
			original.Catalogs = [2][]byte{oldOuter, oldOuter}
			original.Checkpoints = [2][]byte{prior.cp[2], prior.cp[3]}
			input := storageEnrollmentPeakComparisonInput{Catalogs: pair, Evidence: completed.raw, ArmedEvidence: armed.raw, PreviousEvidence: prior.raw, Dispatch: [2][]byte{register, census}, Original: original, CurrentCheckpoints: [2][]byte{armed.cp[3], armed.cp[2]}, CurrentTickets: armed.tickets}
			if _, err := storageEnrollmentPeakCompare(input, armed.record, armed.claim); err != nil {
				t.Fatal("complete unequal final Armed/Completed", err)
			}
			for _, mutation := range []string{"armed-extent", "completed-extent", "packed", "F", "wrong-bank"} {
				badA, badC := a, c
				switch mutation {
				case "armed-extent":
					badA.Extent += 65536
				case "completed-extent":
					badC.Extent += 65536
				case "packed":
					badC.Packed++
				case "F":
					badC.CensusDigest[0] ^= 1
				case "wrong-bank":
					badC.Bank = badA.Bank
				}
				if storageEnrollmentPeakJoinArmed(badC, badA) {
					t.Fatal("unequal extent join refusal", mutation)
				}
				if mutation == "armed-extent" {
					if _, err := storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(badA), armed.record, armed.claim); err == nil {
						t.Fatal("wrong exact Armed file extent")
					}
				}
				if mutation == "completed-extent" || mutation == "packed" || mutation == "F" || mutation == "wrong-bank" {
					if _, err := storageEnrollmentPeakDecodeEvidence(epEvidenceBytes(badC), armed.record, armed.claim); err == nil {
						t.Fatal("wrong per-file frame/packed/F/bank")
					}
				}
			}
		})
	}
}
