package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// All expectations and mutations below are hand-authored. The existing helper
// supplies only its unchanged original source-shaped graph and source operands.
// No v4 frame/P/C/F is accepted or upgraded: this is a synthetic already-v5
// active base. Construction of a real v5 base remains an explicit future gate.
func ownerV5Fixture(t *testing.T, from bool) (storageOwnerV5Catalog, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	shape, _, r, cl := ownerV4Fixture(t, from)
	b, _ := json.Marshal(shape.old())
	var c storageOwnerV5Catalog
	if e := json.Unmarshal(b, &c); e != nil {
		t.Fatal(e)
	}
	c.Version = 5
	c.Sequence = 10
	c.Phase = "active"
	c.TransitionKind = storageOwnerV5Enrollment
	c.TransitionPlan = storagebudget.ID{}
	c.UpgradeOrigin = storageOwnerV5UpgradeOrigin{}
	c.LineagePlans = []storageOwnerV5LineagePlan{}
	c.LineageCompletions = []storageOwnerV5LineageCompletion{}
	c.LineageContinuations = []storageOwnerV5FromContinuation{}
	c.LineageStampCompletions = []storageOwnerV5StampCompletionRow{}
	q := shape.LineagePlans[0]
	p := storageOwnerV5LineagePlan{PlanID: q.PlanID, Installation: q.Installation, QBinding: q.QBinding, Workspace: storageOwnerV5Workspace{q.Workspace.ID, q.Workspace.Generation, q.Workspace.Digest}, PrivateRoot: storageOwnerV5PrivateRoot{q.PrivateRoot.ID, q.PrivateRoot.Generation, q.PrivateRoot.Identity, q.PrivateRoot.SourceDigest}, ProjectObject: q.ProjectObject, PrivateObject: q.PrivateObject, ConfigObject: q.ConfigObject, ProviderRelationDigest: q.ProviderRelationDigest, HistoryIdentity: append([]byte{}, q.HistoryIdentity...), AccountingOwnerKey: append([]byte{}, q.AccountingOwnerKey...), IdentityInitial: append([]byte{}, q.IdentityInitial...), IdentityFromCleared: append([]byte{}, q.IdentityFromCleared...)}
	var e error
	p.CompletionRoom, p.StampRoom, p.ContinuationRoom, e = storageOwnerV5Rooms(p)
	if e != nil {
		t.Fatal(e)
	}
	c = ownerV5Next(t, c, storageOwnerV5Plan, p.PlanID, r, cl)
	c.LineagePlans = append(c.LineagePlans, p)
	return c, r, cl
}
func ownerV5Clone(c storageOwnerV5Catalog) storageOwnerV5Catalog {
	b, _ := json.Marshal(c)
	var v storageOwnerV5Catalog
	_ = json.Unmarshal(b, &v)
	return v
}
func ownerV5Wire(t *testing.T, c storageOwnerV5Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) []byte {
	t.Helper()
	b, e := storageOwnerV5Encode(c, r, cl)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func ownerV5Active(t *testing.T, c storageOwnerV5Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV5Catalog {
	t.Helper()
	b := ownerV5Wire(t, c, r, cl)
	c = ownerV5Clone(c)
	c.Sequence++
	c.Phase = "active"
	copy(c.PreviousCatalogDigest[:], b[32:64])
	return c
}
func ownerV5Next(t *testing.T, c storageOwnerV5Catalog, kind uint8, id storagebudget.ID, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV5Catalog {
	t.Helper()
	b := ownerV5Wire(t, c, r, cl)
	c = ownerV5Clone(c)
	c.Sequence++
	c.Phase = "prepared"
	c.Transition = storagebudget.ID{byte(c.Sequence + 60)}
	c.TransitionKind = kind
	c.TransitionPlan = id
	copy(c.PreviousCatalogDigest[:], b[32:64])
	return c
}
func ownerV5Receipt(n byte, payload []byte, kind uint8) storageOwnerV5Receipt {
	token := storagebudget.Token{Slot: n, ID: storagebudget.ID{n}, Sequence: uint64(n + 10), Generation: 2}
	d := sha256.Sum256(payload)
	if kind == 1 {
		d = storagebudget.Digest{}
	}
	return storageOwnerV5Receipt{OperationID: storagebudget.ID{n}, Terminal: token, PersistedApplied: token, PersistedNextSequence: uint64(n + 11), RetirementEpoch: 7, IntentDigest: storagebudget.Digest{n, 1}, TicketDigest: storagebudget.Digest{n, 2}, PublishedObject: storageOwnerPersistentObject{1, [16]byte{1}, uint64(100 + n), 0, 1, kind}, PublishedBytesDigest: d, PublicationEvidence: storagebudget.Digest{n, 3}}
}
func ownerV5Complete(t *testing.T, c storageOwnerV5Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV5Catalog {
	t.Helper()
	p := c.LineagePlans[0]
	d, e := storageOwnerV5PlanDigest(p)
	if e != nil {
		t.Fatal(e)
	}
	c = ownerV5Next(t, c, storageOwnerV5Completion, p.PlanID, r, cl)
	c.LineageCompletions = append(c.LineageCompletions, storageOwnerV5LineageCompletion{p.PlanID, d, ownerV5Receipt(3, p.IdentityInitial, 2)})
	return c
}
func ownerV5Stamp(t *testing.T, c storageOwnerV5Catalog, disposition uint8, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV5Catalog {
	t.Helper()
	p := c.LineagePlans[0]
	d, e := storageOwnerV5PlanDigest(p)
	if e != nil {
		t.Fatal(e)
	}
	c = ownerV5Next(t, c, storageOwnerV5StampCompletion, p.PlanID, r, cl)
	source := storageOwnerPersistentObject{1, [16]byte{1}, 80, 0, 1, 1}
	target := source
	target.Object = 81
	s := storageOwnerV5StampCompletionRow{PlanID: p.PlanID, PlanDigest: d, StampDisposition: disposition}
	switch disposition {
	case storageOwnerV5Moved:
		s.StampSourceBefore = source
		s.StampTargetAfter = source
		s.StampReceipt = ownerV5Receipt(4, nil, 1)
		s.StampReceipt.PublishedObject = source
	case storageOwnerV5BothPresent:
		s.StampSourceBefore = source
		s.StampTargetBefore = target
		s.StampSourceAfter = source
		s.StampTargetAfter = target
	case storageOwnerV5SourceAbsent:
		s.StampTargetBefore = target
		s.StampTargetAfter = target
	}
	c.LineageStampCompletions = append(c.LineageStampCompletions, s)
	return c
}
func ownerV5Continue(t *testing.T, c storageOwnerV5Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV5Catalog {
	t.Helper()
	p := c.LineagePlans[0]
	s := c.LineageStampCompletions[0]
	c = ownerV5Next(t, c, storageOwnerV5Continuation, p.PlanID, r, cl)
	c.LineageContinuations = append(c.LineageContinuations, storageOwnerV5FromContinuation{PlanID: s.PlanID, PlanDigest: s.PlanDigest, StampDisposition: s.StampDisposition, StampSourceBefore: s.StampSourceBefore, StampTargetBefore: s.StampTargetBefore, StampSourceAfter: s.StampSourceAfter, StampTargetAfter: s.StampTargetAfter, StampReceipt: s.StampReceipt, Rewrite: ownerV5Receipt(5, p.IdentityFromCleared, 2)})
	return c
}
func ownerV5ThroughC(t *testing.T) (storageOwnerV5Catalog, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	c, r, cl := ownerV5Fixture(t, true)
	c = ownerV5Active(t, c, r, cl)
	c = ownerV5Complete(t, c, r, cl)
	return ownerV5Active(t, c, r, cl), r, cl
}

func TestStorageOwnerCatalogV5Codec(t *testing.T) {
	c, r, cl := ownerV5Fixture(t, true)
	raw := ownerV5Wire(t, c, r, cl)
	if string(raw[:8]) != "DSKOWN5\x00" || binary.LittleEndian.Uint32(raw[8:12]) != 5 || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint64(raw[16:24]) != 11 {
		t.Fatal("fixed v5 header")
	}
	got, e := storageOwnerV5Decode(raw, r, cl)
	if e != nil || !reflect.DeepEqual(got, c) {
		t.Fatal("roundtrip", e)
	}
	if _, e = storageOwnerV4Decode(raw, r, cl); e == nil {
		t.Fatal("v4 accepted v5")
	}
	if _, e = storageOwnerDecodeCatalogV3(raw, r, cl); e == nil {
		t.Fatal("v3 accepted v5")
	}
	if _, e = storageOwnerDecodeCatalogV2(raw, r, cl); e == nil {
		t.Fatal("v2 accepted v5")
	}
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"version", func(b []byte) { b[8] = 4 }}, {"header", func(b []byte) { b[12] = 63 }}, {"reserved", func(b []byte) { b[28] = 1 }}, {"checksum", func(b []byte) { b[32] ^= 1 }}, {"padding", func(b []byte) { b[len(b)-1] = 1 }}, {"sequence", func(b []byte) { b[16]++ }}, {"length", func(b []byte) { binary.LittleEndian.PutUint32(b[24:28], math.MaxUint32) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte{}, raw...)
			tc.mutate(b)
			if _, e := storageOwnerV5Decode(b, r, cl); e == nil {
				t.Fatal("mutation accepted")
			}
		})
	}
	b, _ := json.Marshal(c)
	for _, bad := range [][]byte{
		bytes.Replace(b, []byte(`"StampRoom":`), []byte(`"UnknownStampRoom":`), 1),
		bytes.Replace(b, []byte(`"LineageStampCompletions":[]`), []byte(`"LineageStampCompletions":null`), 1),
		bytes.Replace(b, []byte(`"Version":5`), []byte(`"Version":5,"Version":5`), 1),
		append(append([]byte{}, b...), ' '),
	} {
		if storageOwnerV5Preflight(bad) {
			t.Fatal("non-closed payload accepted")
		}
	}
	for _, kind := range []uint8{0, 5, 7} {
		v := ownerV5Clone(c)
		v.TransitionKind = kind
		if v.validate(r, cl) == nil {
			t.Fatal("reserved kind", kind)
		}
	}
	v := ownerV5Clone(c)
	v.UpgradeOrigin.Version = 4
	if v.validate(r, cl) == nil {
		t.Fatal("upgrade accepted")
	}
}

func TestStorageOwnerCatalogV5FinalPlanDomain(t *testing.T) {
	c, r, cl := ownerV5Fixture(t, true)
	p := c.LineagePlans[0]
	d, e := storageOwnerV5PlanDigest(p)
	if e != nil {
		t.Fatal(e)
	}
	q := p
	q.StampRoom++
	changed, e := storageOwnerV5PlanDigest(q)
	if e != nil || changed == d {
		t.Fatal("final stamp pledge omitted")
	}
	shape, _, _, _ := ownerV4Fixture(t, true)
	oldDigest, e := storageOwnerV4PlanDigest(shape.LineagePlans[0])
	if e != nil || oldDigest == d {
		t.Fatal("v4 domain reused")
	}
	c = ownerV5Active(t, c, r, cl)
	c = ownerV5Complete(t, c, r, cl)
	c.LineageCompletions[0].PlanDigest = oldDigest
	if c.validate(r, cl) == nil {
		t.Fatal("old v4 C silently reinterpreted")
	}
	old := ownerV4Wire(t, shape, r, cl)
	if _, e := storageOwnerV5Decode(old, r, cl); e == nil {
		t.Fatal("old frame upgraded")
	}
	if _, e := storageOwnerV5Classify(old, old, r, cl); e == nil {
		t.Fatal("old pair fallback")
	}
}

func TestStorageOwnerCatalogV5SChain(t *testing.T) {
	for _, disposition := range []uint8{1, 2, 3} {
		t.Run(string(rune('0'+disposition)), func(t *testing.T) {
			c, r, cl := ownerV5ThroughC(t)
			before := ownerV5Clone(c)
			c = ownerV5Stamp(t, c, disposition, r, cl)
			if !storageOwnerV5AppendOnlyKind(before, c) || c.validate(r, cl) != nil {
				t.Fatal("append S")
			}
			s := c.LineageStampCompletions[0]
			d, e := storageOwnerV5StampDigest(s)
			if e != nil || d == (storagebudget.Digest{}) {
				t.Fatal("S digest", e)
			}
			c = ownerV5Active(t, c, r, cl)
			stamped := ownerV5Clone(c)
			c = ownerV5Continue(t, c, r, cl)
			if c.validate(r, cl) != nil || !storageOwnerV5Retains(stamped, c) || c.LineageContinuations[0].stamp() != s {
				t.Fatal("F reproduction")
			}
			c = ownerV5Active(t, c, r, cl)
			raw := ownerV5Wire(t, c, r, cl)
			if _, e := storageOwnerV5Decode(raw, r, cl); e != nil {
				t.Fatal(e)
			}
			for _, tc := range []struct {
				name   string
				mutate func(*storageOwnerV5Catalog)
			}{
				{"S-without-C", func(v *storageOwnerV5Catalog) { v.LineageCompletions = []storageOwnerV5LineageCompletion{} }},
				{"F-without-S", func(v *storageOwnerV5Catalog) { v.LineageStampCompletions = []storageOwnerV5StampCompletionRow{} }},
				{"S-duplicate", func(v *storageOwnerV5Catalog) {
					v.LineageStampCompletions = append(v.LineageStampCompletions, v.LineageStampCompletions[0])
				}},
				{"S-wrong-plan", func(v *storageOwnerV5Catalog) { v.LineageStampCompletions[0].PlanDigest[0] ^= 1 }},
				{"F-stamp-drift", func(v *storageOwnerV5Catalog) { v.LineageContinuations[0].StampTargetAfter.Object++ }},
				{"F-receipt-drift", func(v *storageOwnerV5Catalog) { v.LineageContinuations[0].StampReceipt.IntentDigest[0] ^= 1 }},
				{"F-clear-later-applied", func(v *storageOwnerV5Catalog) { v.LineageContinuations[0].Rewrite.PersistedApplied.Sequence++ }},
				{"F-clear-wrong-bytes", func(v *storageOwnerV5Catalog) { v.LineageContinuations[0].Rewrite.PublishedBytesDigest[0] ^= 1 }},
				{"F-reuse-C-operation", func(v *storageOwnerV5Catalog) {
					v.LineageContinuations[0].Rewrite.OperationID = v.LineageCompletions[0].Initial.OperationID
				}},
				{"F-reuse-C-token", func(v *storageOwnerV5Catalog) {
					v.LineageContinuations[0].Rewrite.Terminal = v.LineageCompletions[0].Initial.Terminal
					v.LineageContinuations[0].Rewrite.PersistedApplied = v.LineageCompletions[0].Initial.Terminal
				}},
				{"stamp-pledge", func(v *storageOwnerV5Catalog) { v.LineagePlans[0].StampRoom-- }},
				{"completion-pledge", func(v *storageOwnerV5Catalog) { v.LineagePlans[0].CompletionRoom-- }},
				{"continuation-pledge", func(v *storageOwnerV5Catalog) { v.LineagePlans[0].ContinuationRoom-- }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					v := ownerV5Clone(c)
					tc.mutate(&v)
					if v.validate(r, cl) == nil {
						t.Fatal("invalid chain accepted")
					}
					if _, e := storageOwnerV5ClosureSize(v); e == nil {
						t.Fatal("closure accepted invalid actual row")
					}
				})
			}
			erased := ownerV5Clone(c)
			erased.LineageStampCompletions = []storageOwnerV5StampCompletionRow{}
			if storageOwnerV5Retains(c, erased) {
				t.Fatal("S erasure")
			}
			mutated := ownerV5Clone(c)
			mutated.LineageStampCompletions[0].StampTargetAfter.Object++
			if storageOwnerV5Retains(c, mutated) {
				t.Fatal("S mutation")
			}
		})
	}
	// A structurally valid S is still forbidden without a From-bearing plan.
	no, r, cl := ownerV5Fixture(t, false)
	no = ownerV5Active(t, no, r, cl)
	no = ownerV5Complete(t, no, r, cl)
	no = ownerV5Active(t, no, r, cl)
	p := no.LineagePlans[0]
	d, _ := storageOwnerV5PlanDigest(p)
	no.LineageStampCompletions = []storageOwnerV5StampCompletionRow{{PlanID: p.PlanID, PlanDigest: d, StampDisposition: storageOwnerV5SourceAbsent}}
	if no.validate(r, cl) == nil {
		t.Fatal("S without From")
	}
}

func TestStorageOwnerCatalogV5ClosureStates(t *testing.T) {
	// Each table row names the missing receipts; a real row replaces only its own
	// pledged placeholder. Header widths in the closure are already maximal.
	for _, from := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-From-C-only", true: "From-C-S-F"}[from], func(t *testing.T) {
			c, r, cl := ownerV5Fixture(t, from)
			p := c.LineagePlans[0]
			if p.CompletionRoom == 0 || (p.StampRoom == 0) != (!from) || (p.ContinuationRoom == 0) != (!from) {
				t.Fatal("room combination")
			}
			want, e := storageOwnerV5ClosureSize(c)
			if e != nil {
				t.Fatal(e)
			}
			c = ownerV5Active(t, c, r, cl)
			c = ownerV5Complete(t, c, r, cl)
			b, _ := json.Marshal(c.LineageCompletions[0])
			want += len(b) - int(p.CompletionRoom)
			got, e := storageOwnerV5ClosureSize(c)
			if e != nil || got != want {
				t.Fatal("C retains actual; owes S+F only with From", got, want, e)
			}
			if !from {
				return
			}
			c = ownerV5Active(t, c, r, cl)
			c = ownerV5Stamp(t, c, storageOwnerV5Moved, r, cl)
			b, _ = json.Marshal(c.LineageStampCompletions[0])
			want += len(b) - int(p.StampRoom)
			got, e = storageOwnerV5ClosureSize(c)
			if e != nil || got != want {
				t.Fatal("S retains C+S; owes F", got, want, e)
			}
			c = ownerV5Active(t, c, r, cl)
			c = ownerV5Continue(t, c, r, cl)
			b, _ = json.Marshal(c.LineageContinuations[0])
			want += len(b) - int(p.ContinuationRoom)
			got, e = storageOwnerV5ClosureSize(c)
			if e != nil || got != want {
				t.Fatal("F retains C+S+F; owes none", got, want, e)
			}
			// S is retained through completed reopening and later ordinary enrollment.
			c = ownerV5Active(t, c, r, cl)
			prior := ownerV5Clone(c)
			c = ownerV5Next(t, c, storageOwnerV5Enrollment, storagebudget.ID{}, r, cl)
			if !storageOwnerV5AppendOnlyKind(prior, c) || c.validate(r, cl) != nil || !reflect.DeepEqual(prior.LineageStampCompletions, c.LineageStampCompletions) {
				t.Fatal("later enrollment erased S")
			}
		})
	}
}

func TestStorageOwnerCatalogV5PeerCuts(t *testing.T) {
	c, r, cl := ownerV5ThroughC(t)
	low := ownerV5Wire(t, c, r, cl)
	s := ownerV5Stamp(t, c, storageOwnerV5Moved, r, cl)
	prepared := ownerV5Wire(t, s, r, cl)
	activeCatalog := ownerV5Active(t, s, r, cl)
	active := ownerV5Wire(t, activeCatalog, r, cl)
	blank := make([]byte, storageOwnerCatalogBytes)
	torn := append([]byte{}, active...)
	torn[100] ^= 1
	for _, tc := range []struct {
		name  string
		a, b  []byte
		class storageOwnerV5PeerClass
		phase string
	}{
		{"C-to-S-prepared", low, prepared, storageOwnerV5AdjacentProvisional, "prepared"},
		{"S-prepared-to-active", prepared, active, storageOwnerV5AdjacentProvisional, "active"},
		{"S-prepared-duplicate", prepared, prepared, storageOwnerV5PreparedDuplicatedProvisional, "prepared"},
		{"S-active-duplicate", active, active, storageOwnerV5ActiveDuplicatedProvisional, "active"},
		{"S-prepared-blank", prepared, blank, storageOwnerV5SavedPreparedReceiptProvisional, "prepared"},
		{"S-prepared-torn-active", prepared, torn, storageOwnerV5SavedPreparedReceiptProvisional, "prepared"},
		{"S-active-torn", active, torn, storageOwnerV5ActiveNeedsExactPeer, "active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := storageOwnerV5Classify(tc.a, tc.b, r, cl)
			if e != nil || got.Class != tc.class || got.Catalog.Phase != tc.phase || got.Catalog.TransitionKind != storageOwnerV5StampCompletion {
				t.Fatal("classification", got.Class, e)
			}
			rev, e := storageOwnerV5Classify(tc.b, tc.a, r, cl)
			if e != nil || rev.Class != tc.class || !reflect.DeepEqual(got.Catalog, rev.Catalog) {
				t.Fatal("reversed classification", e)
			}
		})
	}
	for _, tc := range []struct {
		name string
		a, b []byte
	}{
		{"missing-peer", prepared, nil}, {"short-peer", prepared, blank[:100]},
		{"same-sequence-different-S", prepared, func() []byte {
			v := ownerV5Clone(s)
			v.LineageStampCompletions[0].StampReceipt.IntentDigest[0] ^= 1
			return ownerV5Wire(t, v, r, cl)
		}()},
		{"checksum-valid-reserved-peer", prepared, func() []byte {
			b := append([]byte{}, active...)
			b[28] = 1
			h := sha256.New()
			h.Write(b[:32])
			n := binary.LittleEndian.Uint32(b[24:28])
			h.Write(b[64 : 64+int(n)])
			copy(b[32:64], h.Sum(nil))
			return b
		}()},
		{"checksum-valid-padding-peer", prepared, func() []byte { b := append([]byte{}, active...); b[len(b)-1] = 1; return b }()},
		{"both-torn", torn, torn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := storageOwnerV5Classify(tc.a, tc.b, r, cl); e == nil {
				t.Fatal("unsafe peer accepted")
			}
		})
	}
	// A valid v4 peer may never become a torn-v5/bridge operand.
	old, _, _, _ := ownerV4Fixture(t, true)
	oldRaw := ownerV4Wire(t, old, r, cl)
	if _, e := storageOwnerV5Classify(active, oldRaw, r, cl); e == nil {
		t.Fatal("mixed old peer")
	}
}

func TestStorageOwnerCatalogV5MaximaAndClosedDTO(t *testing.T) {
	names := func(v any) string {
		ty := reflect.TypeOf(v)
		fields := make([]string, ty.NumField())
		for i := range fields {
			fields[i] = ty.Field(i).Name
		}
		return strings.Join(fields, ",")
	}
	if got := names(storageOwnerV5StampCompletionRow{}); got != "PlanID,PlanDigest,StampDisposition,StampSourceBefore,StampTargetBefore,StampSourceAfter,StampTargetAfter,StampReceipt" {
		t.Fatal("S declaration order", got)
	}
	if got := names(storageOwnerV5LineagePlan{}); got != "PlanID,Installation,QBinding,Workspace,PrivateRoot,ProjectObject,PrivateObject,ConfigObject,ProviderRelationDigest,HistoryIdentity,AccountingOwnerKey,IdentityInitial,IdentityFromCleared,CompletionRoom,ContinuationRoom,StampRoom" {
		t.Fatal("P declaration order", got)
	}
	c, r, cl := ownerV5Fixture(t, true)
	// Limits are independent. Count maxima are not a simultaneous-fit guarantee.
	oversized := ownerV5Clone(c)
	oversized.LineagePlans[0].HistoryIdentity = []byte(strings.Repeat("h", 4096))
	if storageOwnerV5RowBounds(oversized) {
		t.Fatal("4096-byte plan cap bypassed")
	}
	for _, field := range []string{"LineagePlans", "LineageCompletions", "LineageContinuations", "LineageStampCompletions"} {
		v := ownerV5Clone(c)
		rows := reflect.ValueOf(&v).Elem().FieldByName(field)
		rows.Set(reflect.MakeSlice(rows.Type(), 1025, 1025))
		if storageOwnerV5RowBounds(v) {
			t.Fatal("row-count cap", field)
		}
	}
	if _, e := storageOwnerV5Frame(make([]byte, storageOwnerCatalogBytes-63), 1); e == nil {
		t.Fatal("whole frame cap")
	}
	// Preserve the unchanged source preflight bound even on a new-format payload.
	v := ownerV5Clone(c)
	v.OriginalOwner.HomePath = []byte(strings.Repeat("x", (4<<20)+1))
	b, _ := json.Marshal(v)
	if storageOwnerV5Preflight(b) {
		t.Fatal("source bytes cap")
	}
	// Canonical aggregate scanner refuses before row allocation. Build an array
	// just past the aggregate cap when that precedes the independent count cap.
	for _, g := range []struct {
		field string
		row   any
		limit int
	}{
		{"LineageCompletions", storageOwnerV5CompletionShadow(c.LineagePlans[0]), 1 << 20},
		{"LineageStampCompletions", storageOwnerV5StampShadow(c.LineagePlans[0]), 2 << 20},
		{"LineageContinuations", storageOwnerV5ContinuationShadow(c.LineagePlans[0]), 2 << 20},
	} {
		row, _ := json.Marshal(g.row)
		n := g.limit/(len(row)+1) + 1
		array := []byte("[" + strings.Repeat(string(row)+",", n-1) + string(row) + "]")
		b, _ := json.Marshal(c)
		needle := []byte(`"` + g.field + `":[]`)
		replacement := append([]byte(`"`+g.field+`":`), array...)
		b = bytes.Replace(b, needle, replacement, 1)
		if storageOwnerV5Preflight(b) {
			t.Fatal("aggregate/count cap", g.field)
		}
	}
	// A maximum-width valid directory moved relation and a four-directory
	// read-only relation must each fit both S and the final F pledge.
	p := c.LineagePlans[0]
	source := storageOwnerV5MaxObject(1)
	target := source
	target.Object--
	moved := storageOwnerV5StampCompletionRow{p.PlanID, storageOwnerV5MaxDigest(), 1, source, storageOwnerPersistentObject{}, storageOwnerPersistentObject{}, source, storageOwnerV5MaxReceipt(1)}
	moved.StampReceipt.PublishedObject = source
	both := storageOwnerV5StampCompletionRow{p.PlanID, storageOwnerV5MaxDigest(), 2, source, target, source, target, storageOwnerV5Receipt{}}
	for _, s := range []storageOwnerV5StampCompletionRow{moved, both} {
		b, _ := json.Marshal(s)
		if len(b) > int(p.StampRoom) {
			t.Fatal("maximal S annotations")
		}
		f := storageOwnerV5FromContinuation{s.PlanID, s.PlanDigest, s.StampDisposition, s.StampSourceBefore, s.StampTargetBefore, s.StampSourceAfter, s.StampTargetAfter, s.StampReceipt, storageOwnerV5MaxReceipt(2)}
		b, _ = json.Marshal(f)
		if len(b) > int(p.ContinuationRoom) {
			t.Fatal("maximal F annotations")
		}
	}
	if _, e := storageOwnerV5Decode(ownerV5Wire(t, c, r, cl), r, cl); e != nil {
		t.Fatal(e)
	}
}

func TestStorageOwnerCatalogV5SeparateEffectsAndSharedSpentKeys(t *testing.T) {
	baseline := storageOwnerV4CatalogEffects()
	proposed := storageOwnerV5ProposedStampEffects()
	seen := map[string]bool{}
	var totalBytes, totalInodes uint64
	for _, e := range baseline {
		seen[e.Key] = true
	}
	if len(baseline) != 15 {
		t.Fatal("accepted15 changed")
	}
	for _, e := range proposed {
		if seen[e.Key] || !strings.HasPrefix(e.Key, "m1-proposed/stamp-anchor/") || e.Bytes != 8388608 || e.Files != 0 || e.Directories != 0 || e.Metadata != 4 || e.MaxPerLaunch != 1 || e.Parent != storageOwnerV4OriginalConfigParent {
			t.Fatal("proposed operand", e)
		}
		seen[e.Key] = true
		totalBytes += e.PolicyBytes
		totalInodes += e.PolicyInodes
	}
	if totalBytes != 69206016 || totalInodes != 8 {
		t.Fatal("four-effect bound")
	}
	fresh, e := storageOwnerV5DuplicateOperands(nil)
	if e != nil || fresh != ([2]string{"lin4/bridge-duplicate/publish", "lin4/bridge-duplicate/persist"}) {
		t.Fatal("shared exact keys")
	}
	for _, key := range fresh {
		spent := map[string]bool{key: true}
		if _, e := storageOwnerV5DuplicateOperands(spent); e == nil {
			t.Fatal("spent/failed key reused", key)
		}
		if !spent[key] || len(spent) != 1 {
			t.Fatal("ledger mutated")
		}
	}
}

func TestStorageOwnerCatalogV5MixedPendingRooms(t *testing.T) {
	// Four simultaneous plans: P-only, C, S, F. Every actual row survives while
	// the missing set is (C+S+F), (S+F), (F), (). Distinct receipts share no token.
	c, r, cl := ownerV5Fixture(t, true)
	template := c.LineagePlans[0]
	c.LineagePlans = []storageOwnerV5LineagePlan{}
	c.TransitionKind = storageOwnerV5Enrollment
	c.TransitionPlan = storagebudget.ID{}
	for i := 0; i < 4; i++ {
		p := template
		p.PlanID = storagebudget.ID{byte(40 + i)}
		p.CompletionRoom, p.StampRoom, p.ContinuationRoom, _ = storageOwnerV5Rooms(p)
		c.LineagePlans = append(c.LineagePlans, p)
		d, _ := storageOwnerV5PlanDigest(p)
		if i >= 1 {
			c.LineageCompletions = append(c.LineageCompletions, storageOwnerV5LineageCompletion{p.PlanID, d, ownerV5Receipt(byte(10+i), p.IdentityInitial, 2)})
		}
		if i >= 2 {
			c.LineageStampCompletions = append(c.LineageStampCompletions, storageOwnerV5StampCompletionRow{PlanID: p.PlanID, PlanDigest: d, StampDisposition: storageOwnerV5SourceAbsent})
		}
		if i == 3 {
			s := c.LineageStampCompletions[len(c.LineageStampCompletions)-1]
			c.LineageContinuations = append(c.LineageContinuations, storageOwnerV5FromContinuation{PlanID: s.PlanID, PlanDigest: s.PlanDigest, StampDisposition: s.StampDisposition, Rewrite: ownerV5Receipt(20, p.IdentityFromCleared, 2)})
		}
	}
	if c.validate(r, cl) != nil {
		t.Fatal("mixed pending graph")
	}
	before := ownerV5Clone(c)
	n, e := storageOwnerV5ClosureSize(c)
	if e != nil || n <= 0 || !reflect.DeepEqual(before, c) {
		t.Fatal("closure lost/mutated actual rows", e)
	}
	// Adding a new unrelated plan cannot use the other plans' room.
	p := template
	p.PlanID = storagebudget.ID{44}
	p.CompletionRoom, p.StampRoom, p.ContinuationRoom, _ = storageOwnerV5Rooms(p)
	c.LineagePlans = append(c.LineagePlans, p)
	later, e := storageOwnerV5ClosureSize(c)
	if e != nil || later <= n+int(p.CompletionRoom+p.StampRoom+p.ContinuationRoom) {
		t.Fatal("unrelated plan consumed existing pledge", n, later, e)
	}
}

func TestStorageOwnerCatalogV5StampReceiptPredicates(t *testing.T) {
	c, r, cl := ownerV5ThroughC(t)
	c = ownerV5Stamp(t, c, storageOwnerV5Moved, r, cl)
	for _, tc := range []struct {
		name   string
		mutate func(*storageOwnerV5StampCompletionRow)
	}{
		{"missing-receipt", func(s *storageOwnerV5StampCompletionRow) { s.StampReceipt = storageOwnerV5Receipt{} }},
		{"later-applied-history", func(s *storageOwnerV5StampCompletionRow) { s.StampReceipt.PersistedApplied.Sequence++ }},
		{"next-not-greater", func(s *storageOwnerV5StampCompletionRow) {
			s.StampReceipt.PersistedNextSequence = s.StampReceipt.Terminal.Sequence
		}},
		{"wrong-native-target", func(s *storageOwnerV5StampCompletionRow) { s.StampReceipt.PublishedObject.Object++ }},
		{"missing-publication-evidence", func(s *storageOwnerV5StampCompletionRow) { s.StampReceipt.PublicationEvidence = storagebudget.Digest{} }},
		{"missing-intent", func(s *storageOwnerV5StampCompletionRow) { s.StampReceipt.IntentDigest = storagebudget.Digest{} }},
		{"missing-ticket", func(s *storageOwnerV5StampCompletionRow) { s.StampReceipt.TicketDigest = storagebudget.Digest{} }},
		{"wrong-readonly-with-receipt", func(s *storageOwnerV5StampCompletionRow) { s.StampDisposition = storageOwnerV5SourceAbsent }},
		{"unknown-disposition", func(s *storageOwnerV5StampCompletionRow) { s.StampDisposition = 4 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := ownerV5Clone(c)
			tc.mutate(&v.LineageStampCompletions[0])
			if v.validate(r, cl) == nil {
				t.Fatal("bad stamp fact accepted")
			}
		})
	}
	// The new scanner delegates original source accumulation, including every
	// repeated occurrence, to exactly the original 4MiB counter.
	raw := []byte(`"eA=="`)
	scanner := storageOwnerV5Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}, sourceBytes: (4 << 20) - 1}}
	if !scanner.value(reflect.TypeOf([]byte{}), "Path") || scanner.old.sourceBytes != 4<<20 {
		t.Fatal("inclusive source cap")
	}
	scanner = storageOwnerV5Scanner{old: storageOwnerCatalogScanner{storageOwnerJSONPreflight: storageOwnerJSONPreflight{raw: raw}, sourceBytes: 4 << 20}}
	if scanner.value(reflect.TypeOf([]byte{}), "Path") {
		t.Fatal("source aggregate overflow")
	}
}
