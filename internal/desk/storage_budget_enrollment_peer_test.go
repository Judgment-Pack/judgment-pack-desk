package desk

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageEnrollmentRetainedOriginalCatalogPeer(t *testing.T) {
	low, record, claim := ownerAdvisoryFixture(false)
	low.Sequence = 3
	low.PreviousCatalogDigest = storagebudget.Digest{77}
	ownerCatalogBeforeAll(&low)
	ownerCatalogSeal(&low, &record, &claim)
	lowRaw := ownerAdvisoryWire(low)
	high := ownerCatalogClone(low)
	high.Sequence = 4
	high.Phase = "active"
	ownerCatalogLink(&high, lowRaw)
	highRaw := ownerAdvisoryWire(high)
	for _, kind := range []string{"linked-high0", "linked-high1", "duplicated"} {
		t.Run(kind, func(t *testing.T) {
			raws := [2][]byte{highRaw, lowRaw}
			bank := byte(0)
			if kind == "linked-high1" {
				raws = [2][]byte{lowRaw, highRaw}
				bank = 1
			}
			if kind == "duplicated" {
				raws = [2][]byte{highRaw, highRaw}
				bank = 1
			}
			scope := epScope(1, true, record, [2]uint64{}, [2]uint64{262144, 262144})
			storageEnrollmentPeakPut16(scope[:2], 2)
			scope[322] = bank
			storageEnrollmentPeakPut64(scope[1624:1632], 4)
			digest := sha256.Sum256(highRaw)
			copy(scope[88:120], digest[:])
			if err := storageEnrollmentPeakRetainOriginalPeer(&scope, raws, record, claim); err != nil {
				t.Fatal(err)
			}
			// Changing the physical catalog selector also changes each catalog bank's
			// attributed schedule ceiling. Derive those fields from the independent
			// handwritten plan, rather than changing only the selector in epScope.
			digest = scope.policyDigest()
			copy(scope[184:216], digest[:])
			if bank == 0 {
				if _, err := storageEnrollmentPeakDecodeScope(scope[:]); err == nil {
					t.Fatal("stale bank1 schedule accepted for bank0")
				}
			}
			var target [10]uint64
			for _, g := range epGroups(1, [2]uint64{262144, 262144}, bank, 255, 255, 0) {
				target[g.Target-1] += epEstimate(g).Bytes
			}
			for i := 0; i < 8; i++ {
				storageEnrollmentPeakPut64(scope[1360+i*32+20:1360+i*32+28], target[i])
			}
			digest = scope.policyDigest()
			copy(scope[184:216], digest[:])
			if _, err := storageEnrollmentPeakDecodeScope(scope[:]); err != nil {
				t.Fatal(err)
			}
			// The physical original peer is now overwritten. Only retained immutable
			// operands plus canonical v3 encoding may reconstruct the original pair.
			saved := [2][]byte{append([]byte{}, raws[0]...), append([]byte{}, raws[1]...)}
			raws[1-bank] = make([]byte, 8388608)
			restored, selected, err := storageEnrollmentPeakRestoreOriginalPeer(scope, highRaw, record, claim)
			if err != nil || selected != bank || !bytes.Equal(restored[0], saved[0]) || !bytes.Equal(restored[1], saved[1]) {
				t.Fatal("original pair lost after peer overwrite", err)
			}
			for _, mutation := range []string{"peer-digest", "prior-digest", "missing-prior", "kind", "bank", "reserved", "v1"} {
				t.Run(mutation, func(t *testing.T) {
					bad := scope
					switch mutation {
					case "peer-digest":
						bad[1640] ^= 1
					case "prior-digest":
						bad[1672] ^= 1
					case "missing-prior":
						clear(bad[1672:1704])
					case "kind":
						bad[1704] = 0
					case "bank":
						bad[322] = 1 - bank
					case "reserved":
						bad[1705] = 1
					case "v1":
						storageEnrollmentPeakPut16(bad[:2], 1)
						clear(bad[1640:])
					}
					if _, _, err := storageEnrollmentPeakRestoreOriginalPeer(bad, highRaw, record, claim); err == nil {
						t.Fatal("accepted altered or unavailable original operand")
					}
				})
			}
			successor := scope
			successor[3] = 2
			successor[322] = 0
			if !storageEnrollmentPeakOriginalPeerRetained(successor, scope) {
				t.Fatal("immediate-before selector changed founding evidence")
			}
			successor[1640] ^= 1
			if storageEnrollmentPeakOriginalPeerRetained(successor, scope) {
				t.Fatal("lost founding peer evidence")
			}
		})
	}
}

func TestStorageEnrollmentRetainedOriginalCheckpointPeer(t *testing.T) {
	f := epEvidence(t, 1)
	// epEvidence deliberately authors framing and section bytes, not decoded
	// DTOs. Supply the complete selected projection independently from its
	// handwritten view, original dictionary order and original ticket pages.
	view, viewRaw := epView(f.before, f.q[0])
	views := []storageEnrollmentPeakView{view}
	if !bytes.Equal(viewRaw, f.sections[3]) {
		t.Fatal("independent selected view drift")
	}
	selected := storageEnrollmentPeakCheckpointProjection{Q: f.q[0], Checkpoint: append([]byte{}, f.cp[0]...), View: 0, RootRefs: append([]uint16{}, view.DefinitionRefs[2]...), WorkspaceRefs: append([]uint16{}, view.DefinitionRefs[3]...), FilesystemRefs: append([]uint16{}, view.DefinitionRefs[0]...), ConstraintRefs: append([]uint16{}, view.DefinitionRefs[1]...)}
	for slot := 0; slot < 64; slot++ {
		for bank := 0; bank < 2; bank++ {
			selected.Tickets[slot][bank] = sha256.Sum256(f.tickets[bank][slot*16384 : (slot+1)*16384])
		}
	}
	selected.Checkpoints = [2]storagebudget.Digest{sha256.Sum256(f.cp[0]), sha256.Sum256(f.cp[0])}
	original, err := storageEnrollmentPeakWriteCheckpointV1(selected, f.after, views, f.scope)
	if err != nil || !bytes.Equal(original, f.sections[6]) {
		t.Fatal("complete selected projection differs from handwritten checkpoint", err)
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(f.record.Installation), Binding: storageEnrollmentPeakQDigest(f.record.QBinding)}
	for _, bank := range []byte{0, 1} {
		t.Run(string(rune('0'+bank)), func(t *testing.T) {
			p := selected
			p.Bank = bank
			// The independent older peer deliberately has different root definitions
			// which are not dictionary rows in the current catalog.
			peer := *f.q[0]
			peer.Generation--
			peer.Roots[0].Locator = "historical-independent-locator"
			peer.Workspaces[0].Generation++
			peerRaw, err := storageEnrollmentPeakQEncodeCheckpoint(&peer, anchor)
			if err != nil {
				t.Fatal(err)
			}
			p.PeerCheckpoint = peerRaw
			p.Checkpoints[bank] = sha256.Sum256(f.cp[0])
			p.Checkpoints[1-bank] = sha256.Sum256(peerRaw)
			scope := f.scope
			storageEnrollmentPeakPut16(scope[:2], 2)
			scope[325] = bank
			copy(scope[120:152], p.Checkpoints[bank][:])
			raw, err := storageEnrollmentPeakWriteCheckpoint(p, f.after, views, scope)
			if err != nil {
				t.Fatal(err)
			}
			got, err := storageEnrollmentPeakReadCheckpoint(raw, f.after, views, scope)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := storageEnrollmentPeakRestoreOriginalCheckpoints(got, f.after)
			if err != nil || !bytes.Equal(restored[bank], f.cp[0]) || !bytes.Equal(restored[1-bank], peerRaw) {
				t.Fatal("exact original pair not retained", err)
			}
			// Q2 has overwritten both physical original checkpoint pages; neither is
			// a permitted substitute for the exact immutable original pair.
			current := [2][]byte{f.cp[1], f.cp[2]}
			if bytes.Equal(current[bank], restored[bank]) || bytes.Equal(current[1-bank], restored[1-bank]) {
				t.Fatal("fixture does not overwrite original pair")
			}
			for _, offset := range []int{0, 4, 8, len(raw) - 1} {
				bad := append([]byte{}, raw...)
				bad[offset] ^= 128
				if _, err := storageEnrollmentPeakReadCheckpoint(bad, f.after, views, scope); err == nil {
					t.Fatalf("accepted checkpoint corruption%d", offset)
				}
			}
			oldScope := scope
			storageEnrollmentPeakPut16(oldScope[:2], 1)
			old, err := storageEnrollmentPeakWriteCheckpointV1(p, f.after, views, oldScope)
			if err != nil {
				t.Fatal(err)
			}
			v1, err := storageEnrollmentPeakReadCheckpoint(old, f.after, views, oldScope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := storageEnrollmentPeakRestoreOriginalCheckpoints(v1, f.after); err == nil {
				t.Fatal("v1 invented missing peer")
			}
		})
	}
}

// These cases use the existing handwritten selected view/checkpoint pages and
// an independently different historical peer. No timing or call-count assertion
// substitutes for exact bytes, complete Q state, or the strict refusal boundary.
func TestStorageEnrollmentCheckpointWriterValidationContract(t *testing.T) {
	for _, name := range []string{
		"legacy", "peer-bank0", "peer-bank1", "selected-mismatch-before-peer-invalid",
		"peer-invalid", "retained-digest-mismatch", "slot-content-mismatch", "alias-independent",
	} {
		t.Run(name, func(t *testing.T) {
			if name == "slot-content-mismatch" {
				_, retained, _ := compositionRetiredOriginEvidence2(t)
				p := retained.Checkpoint
				originalSlots := storageEnrollmentOperandClone(retained.Views[0].Slots)
				if _, err := storageEnrollmentPeakWriteCheckpoint(p, retained.Inner.Catalog, retained.Views, retained.Scope); err != nil {
					t.Fatal("independent composed retained checkpoint refused", err)
				}
				views := storageEnrollmentOperandClone(retained.Views)
				changed := false
				for i := range views[0].Slots {
					ticket := &views[0].Slots[i].Ticket
					if len(ticket.Components) == 0 || len(ticket.Changes) == 0 {
						continue
					}
					ticket.Components[0].Physical.Bytes++
					if _, err := storageEnrollmentPeakWriteTicket(*ticket); err != nil {
						t.Fatal("mutation must remain a valid nonempty ticket", err)
					}
					changed = true
					break
				}
				if !changed {
					t.Fatal("fixture lost its nonempty retained slot")
				}
				if _, err := storageEnrollmentPeakWriteCheckpoint(p, retained.Inner.Catalog, views, retained.Scope); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("accepted changed nonempty retained slot", err)
				}
				if !reflect.DeepEqual(retained.Views[0].Slots, originalSlots) {
					t.Fatal("writer changed one or more original retained slots")
				}
				return
			}
			f := epEvidence(t, 1)
			view, viewRaw := epView(f.before, f.q[0])
			if !bytes.Equal(viewRaw, f.sections[3]) {
				t.Fatal("handwritten selected view drift")
			}
			views := []storageEnrollmentPeakView{view}
			p := storageEnrollmentPeakCheckpointProjection{Q: f.q[0], Checkpoint: append([]byte{}, f.cp[0]...), View: 0,
				RootRefs: append([]uint16{}, view.DefinitionRefs[2]...), WorkspaceRefs: append([]uint16{}, view.DefinitionRefs[3]...),
				FilesystemRefs: append([]uint16{}, view.DefinitionRefs[0]...), ConstraintRefs: append([]uint16{}, view.DefinitionRefs[1]...)}
			for slot := range p.Tickets {
				for bank := range p.Tickets[slot] {
					p.Tickets[slot][bank] = sha256.Sum256(f.tickets[bank][slot*16384 : (slot+1)*16384])
				}
			}
			bank := byte(0)
			if name == "peer-bank1" {
				bank = 1
			}
			p.Bank = bank
			p.Checkpoints = [2]storagebudget.Digest{sha256.Sum256(f.cp[0]), sha256.Sum256(f.cp[0])}
			scope := f.scope
			peer := *f.q[0]
			peer.Generation--
			peer.Roots[0].Locator = "independent-retained-peer"
			peer.Workspaces[0].Generation++
			// epQBytes authors the original page independently of the codec.
			peerRaw := epQBytes(&peer, f.record)
			if name != "legacy" {
				p.PeerCheckpoint = append([]byte{}, peerRaw...)
				p.Checkpoints[1-bank] = sha256.Sum256(peerRaw)
				storageEnrollmentPeakPut16(scope[:2], 2)
				scope[325] = bank
				copy(scope[120:152], p.Checkpoints[bank][:])
			}
			switch name {
			case "selected-mismatch-before-peer-invalid":
				q := *p.Q
				q.Generation++ // Structurally valid Q; differs from the retained view.
				p.Q = &q
				p.PeerCheckpoint[0] ^= 128
			case "peer-invalid":
				p.PeerCheckpoint[0] ^= 128
			case "retained-digest-mismatch":
				p.Checkpoints[1-bank][0] ^= 1
			}
			before := storageEnrollmentOperandClone(p)
			beforeViews := storageEnrollmentOperandClone(views)
			raw, err := storageEnrollmentPeakWriteCheckpoint(p, f.after, views, scope)
			if !reflect.DeepEqual(p, before) || !reflect.DeepEqual(views, beforeViews) {
				t.Fatal("writer mutated caller checkpoint or one of its complete64 slots")
			}
			switch name {
			case "selected-mismatch-before-peer-invalid":
				if !errors.Is(err, storagebudget.ErrBounds) || errors.Is(err, storagebudget.ErrUnknown) {
					t.Fatal("selected failure lost priority over malformed peer", err)
				}
				return
			case "peer-invalid":
				if !errors.Is(err, storagebudget.ErrUnknown) {
					t.Fatal("valid selected checkpoint accepted malformed peer", err)
				}
				return
			case "retained-digest-mismatch":
				if !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("accepted changed original peer digest", err)
				}
				return
			}
			if err != nil {
				t.Fatal("handwritten checkpoint refused", err)
			}
			// Independent selected framing: bank and the two original page digests
			// precede64 ticket pairs,64 selectors and the U16 view index.
			expected := epCheckpoint(f.before, f.q[0], f.cp[0], f.tickets)
			offset := len(expected) - (1 + 2*32 + 64*2*32 + 64 + 2)
			expected[offset] = bank
			copy(expected[offset+1:offset+33], p.Checkpoints[0][:])
			copy(expected[offset+33:offset+65], p.Checkpoints[1][:])
			selectedRaw := raw
			if name != "legacy" {
				if len(raw) < 8 {
					t.Fatal("writer returned missing frame")
				}
				n := int(storageEnrollmentPeakU32(raw[:4]))
				if len(raw) < 8+n {
					t.Fatal("writer returned truncated frame")
				}
				selectedRaw = raw[8 : 8+n]
			}
			if !bytes.Equal(selectedRaw, expected) {
				t.Fatal("selected bytes differ from handwritten original framing")
			}
			got, err := storageEnrollmentPeakReadCheckpoint(raw, f.after, views, scope)
			if err != nil || !reflect.DeepEqual(got.Q, f.q[0]) || got.Tickets != p.Tickets || got.TicketBanks != p.TicketBanks {
				t.Fatal("strict read changed full Q or any of64 original ticket slots", err)
			}
			if name == "legacy" {
				standalone, err := storageEnrollmentPeakWriteCheckpointV1(p, f.after, views, scope)
				if err != nil || !bytes.Equal(standalone, f.sections[6]) || !bytes.Equal(raw, standalone) {
					t.Fatal("standalone V1 behavior changed", err)
				}
				return
			}
			restored, err := storageEnrollmentPeakRestoreOriginalCheckpoints(got, f.after)
			if err != nil || !bytes.Equal(restored[bank], f.cp[0]) || !bytes.Equal(restored[1-bank], peerRaw) {
				t.Fatal("complete original selected/peer pages changed", err)
			}
			if name == "alias-independent" {
				beforeRaw := append([]byte{}, raw...)
				got.Q.Roots[0].Locator = "mutated-returned-value"
				got.Raw[0] ^= 1
				got.Checkpoint[0] ^= 1
				got.PeerCheckpoint[0] ^= 1
				got.RootRefs[0]++
				if !bytes.Equal(raw, beforeRaw) || !reflect.DeepEqual(p, before) || !reflect.DeepEqual(views, beforeViews) {
					t.Fatal("returned decode aliases writer inputs or frame")
				}
				again, err := storageEnrollmentPeakReadCheckpoint(raw, f.after, views, scope)
				if err != nil || !reflect.DeepEqual(again.Q, f.q[0]) || again.Tickets != p.Tickets {
					t.Fatal("returned mutation escaped into strict reread", err)
				}
				bad := append([]byte{}, raw...)
				bad[8] ^= 1 // Selected generation no longer equals its original view.
				if _, err := storageEnrollmentPeakReadCheckpoint(bad, f.after, views, scope); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("external reader reused selected validation after changed bytes", err)
				}
			}
		})
	}
}

func TestStorageEnrollmentIndependentCheckpointCompactWidths(t *testing.T) {
	f := epEvidence(t, 1)
	q := *f.q[0]
	q.Roots[0].Locator = strings.Repeat("x", 256)
	raw, err := storageEnrollmentPeakWritePeerCheckpoint(&q)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := storageEnrollmentPeakReadPeerCheckpoint(raw)
	if err != nil || !reflect.DeepEqual(decoded, &q) {
		t.Fatal("lossless maximum locator", err)
	}
	// Exact maximum follows the closed encoder fields; every populated root,
	// workspace, filesystem and constraint contributes these scalar widths.
	max := 721 + 8 + 4096*726 + 1024*483 + 128*123 + 512*75 + 64*40
	if max != 3525721 {
		t.Fatal("independent peer scalar bound drift", max)
	}
	if max != storageEnrollmentPeakPeerCheckpointMax {
		t.Fatal("independent peer constant bound drift", max, storageEnrollmentPeakPeerCheckpointMax)
	}
	total := uint64(65536)
	s := f.scope
	storageEnrollmentPeakPut16(s[:2], 2)
	for i := 0; i < 9; i++ {
		total = (total + 7) &^ uint64(7)
		total += storageEnrollmentPeakSectionMaximum(s, i)
	}
	if total != 70983680 || (total+65535)&^uint64(65535) != 71041024 || storageEnrollmentPeakEvidenceMax-((total+65535)&^uint64(65535)) != 262144 {
		t.Fatal("fixed whole-evidence bound changed", total)
	}
	if storageEnrollmentPeakSectionMaximum(f.scope, 6) != 2097152 || storageEnrollmentPeakSectionMaximum(s, 6) != 4718592 {
		t.Fatal("versioned section limit")
	}
	q.Roots[0].Locator += "x"
	if _, err := storageEnrollmentPeakWritePeerCheckpoint(&q); err == nil {
		t.Fatal("overlong locator")
	}
}

func TestStorageEnrollmentIndependentPeerRootHandwrittenFields(t *testing.T) {
	f := epEvidence(t, 1)
	want := f.q[0].Roots[0]
	want.Locator = strings.Repeat("r", 256)
	// Independent exact root order, with a bounded U16-prefixed Locator between
	// Generation and Known. No product writer constructs these expected bytes.
	w := epBytes{}
	w.fixed(want.ID)
	w.fixed(want.Owner)
	w.fixed(want.Identity)
	w.fixed(want.Filesystem)
	w.fixed(want.Workspace)
	w.fixed(want.Generation)
	w.data([]byte(want.Locator))
	w.fixed(want.Known)
	w.fixed(want.Usage)
	w.fixed(want.CapacityCount)
	w.fixed(want.CapacityRefs)
	w.fixed(want.CapacityBinding)
	if w.Len() != 724 {
		t.Fatal("root scalar width drift", w.Len())
	}
	raw := w.Bytes()
	reader := storageEnrollmentPeakReader{raw: raw}
	var got storageEnrollmentPeakQRoot
	storageEnrollmentPeakReadPeerRoot(&reader, &got)
	if !reader.done() || !reflect.DeepEqual(got, want) {
		t.Fatal("handwritten root fields not retained")
	}
	for _, mutation := range []string{"short", "locator-length", "bool"} {
		bad := append([]byte{}, raw...)
		switch mutation {
		case "short":
			bad = bad[:len(bad)-1]
		case "locator-length":
			storageEnrollmentPeakPut16(bad[120:122], 257)
		case "bool":
			bad[378] = 2
		}
		reader = storageEnrollmentPeakReader{raw: bad}
		got = storageEnrollmentPeakQRoot{}
		storageEnrollmentPeakReadPeerRoot(&reader, &got)
		if reader.done() {
			t.Fatal("accepted malformed handwritten root", mutation)
		}
	}
	reader = storageEnrollmentPeakReader{raw: raw}
	storageEnrollmentPeakReadFixed(&reader, reflect.ValueOf(&got).Elem())
	if !reader.bad {
		t.Fatal("generic fixed reader acquired dynamic-string authority")
	}
}

func TestStorageEnrollmentIndependentCheckpointMaximumCardinality(t *testing.T) {
	f := epEvidence(t, 1)
	q := *f.q[0]
	id := func(n int) storageEnrollmentPeakQID { return storageEnrollmentPeakQID{byte(n >> 8), byte(n)} }
	digest := func(n int) storageEnrollmentPeakQDigest {
		return storageEnrollmentPeakQDigest{byte(n >> 8), byte(n), 123}
	}
	rows := map[storageEnrollmentPeakQID]storageEnrollmentPeakQCapacityConstraint{}
	for i := range q.CapacityConstraints {
		v := storageEnrollmentPeakQCapacityConstraint{ID: id(i + 1), Identity: digest(i + 1), Generation: 1, Resources: 3}
		q.CapacityConstraints[i] = v
		rows[v.ID] = v
	}
	for i := range q.Filesystems {
		q.Filesystems[i] = storageEnrollmentPeakQFilesystem{ID: id(i + 1), Identity: digest(i + 1), Generation: 1, InodeModel: 1, CapabilityEvidence: digest(i + 500), ByteConstraint: id(1), InodeConstraint: id(2)}
	}
	for i := range q.Workspaces {
		q.Workspaces[i] = storageEnrollmentPeakQWorkspace{ID: id(i + 1), Generation: 1, Limits: q.Policy.Workspace}
	}
	for i := range q.Roots {
		v := storageEnrollmentPeakQRoot{ID: id(i + 1), Owner: storageEnrollmentPeakQDigest(f.record.QBinding), Identity: digest(i + 1), Filesystem: q.Filesystems[i%128].ID, Workspace: q.Workspaces[i%1024].ID, Generation: 1, Locator: strings.Repeat("r", 256), CapacityCount: 4}
		for j := range v.CapacityRefs {
			v.CapacityRefs[j] = storageEnrollmentPeakQCapacityRef{Constraint: id(j + 1), Generation: 1}
		}
		var err error
		v.CapacityBinding, err = storageEnrollmentPeakQCapacityBinding(v, q.Filesystems[i%128], rows)
		if err != nil {
			t.Fatal(err)
		}
		q.Roots[i] = v
	}
	compact, err := storageEnrollmentPeakWritePeerCheckpoint(&q)
	if err != nil {
		t.Fatal(err)
	}
	if len(compact) != 3525721 {
		t.Fatalf("maximum encoder fields occupy%d, want3525721", len(compact))
	}
	decoded, err := storageEnrollmentPeakReadPeerCheckpoint(compact)
	if err != nil || !reflect.DeepEqual(decoded, &q) {
		t.Fatal("maximum independent peer not lossless", err)
	}
	anchor := storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(f.record.Installation), Binding: storageEnrollmentPeakQDigest(f.record.QBinding)}
	original, err := storageEnrollmentPeakQEncodeCheckpoint(&q, anchor)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := storageEnrollmentPeakQEncodeCheckpoint(decoded, anchor)
	if err != nil || !bytes.Equal(original, restored) {
		t.Fatal("full original checkpoint digest operand lost", err)
	}
	if _, err := storageEnrollmentPeakReadPeerCheckpoint(append(compact, 0)); err == nil {
		t.Fatal("accepted over-maximum peer")
	}
}

// Handwritten semantic versions share operands, never policy identity. These
// vectors are not native qualification, runtime admission or migration evidence.
func TestStorageEnrollmentCapacityEstimateScopeIdentityRetainsDebt(t *testing.T) {
	_, record, _ := ownerAdvisoryFixture(false)
	legacy := epScope(1, true, record, [2]uint64{}, [2]uint64{262144, 262144})
	epSet16(legacy[:2], 2)
	legacy[1640], legacy[1704] = 1, 1
	seal := func(s *storageEnrollmentPeakScope, domain string) {
		t.Helper()
		normal := *s
		clear(normal[184:216])
		normal[321] = 0
		clear(normal[1080:1220])
		d := epHash(domain, normal[:])
		copy(s[184:216], d[:])
	}
	seal(&legacy, "desk-enrollment-control-peak-v2")
	if _, err := storageEnrollmentPeakDecodeScope(legacy[:]); err != nil {
		t.Fatal("legacy debt is no longer inspectable", err)
	}
	estimate := legacy
	epSet16(estimate[:2], 3)
	estimate[2] = 2
	seal(&estimate, "desk-enrollment-control-capacity-estimate-v1")
	if _, err := storageEnrollmentPeakDecodeScope(estimate[:]); err != nil {
		t.Fatal("explicit estimate identity refused", err)
	}
	if estimate.policyDigest() == legacy.policyDigest() || estimate.requireCapacityEstimate() != nil {
		t.Fatal("old and new policy identities confused")
	}
	reservation := storageEnrollmentReservation{scope: legacy}
	before := reservation
	if !errors.Is(reservation.scope.requireCapacityEstimate(), errStorageEnrollmentPeakMigration) || reservation != before {
		t.Fatal("refusal converted or forgave original debt")
	}
	if !bytes.Equal(estimate[328:408], legacy[328:408]) || !bytes.Equal(estimate[440:1080], legacy[440:1080]) || !bytes.Equal(estimate[1360:1640], legacy[1360:1640]) {
		t.Fatal("estimate identity raised P, caps, attribution or native subjects")
	}
	for _, mutate := range []func(*storageEnrollmentPeakScope){
		func(s *storageEnrollmentPeakScope) { s[2] = 1 },
		func(s *storageEnrollmentPeakScope) { epSet16(s[:2], 2) },
		func(s *storageEnrollmentPeakScope) { epSet32(s[1636:1640], 23) },
		func(s *storageEnrollmentPeakScope) { clear(s[1640:1672]) },
	} {
		bad := estimate
		mutate(&bad)
		if _, err := storageEnrollmentPeakDecodeScope(bad[:]); err == nil {
			t.Fatal("policy, subject or original-proof mutation accepted")
		}
	}
}
