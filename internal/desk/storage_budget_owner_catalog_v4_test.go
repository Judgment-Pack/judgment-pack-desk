package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These are handwritten synthetic detached values, not captured live files or
// claims of factual publication/custody. Original fixture semantics stay v3.
func ownerV4Fixture(t *testing.T, from bool) (storageOwnerV4Catalog, []byte, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	old, r, cl := ownerAdvisoryFixture(false)
	// Author the source-shaped standalone enrollment before constructing any
	// bridge: inspectProject uses OwnerKind3, the retained startup key and its
	// derived workspace ID; .desk-private is Role6/Control with no parent or
	// marker selector. The directory is empty in this original fixture.
	key := []byte(digestOf([]byte("/project")))
	priorID := old.Workspaces[0].Definition.ID
	workspaceID := storageDerivedID("desk-storage-workspace-v2", old.Installation, key)
	old.Workspaces[0].Definition.ID = workspaceID
	old.Workspaces[0].OwnerKind = 3
	old.Workspaces[0].OwnerKey = key
	for i := range old.Roots {
		row := &old.Roots[i]
		if row.Definition.Workspace == priorID {
			row.Definition.Workspace = workspaceID
		}
		for j, id := range row.References {
			if id == priorID {
				row.References[j] = workspaceID
			}
		}
	}
	private := &old.Roots[2]
	private.Role = 6
	private.Category = storagebudget.Control
	private.Source = storageOwnerRootSource{Kind: 6, OwnerKey: append([]byte{}, key...), Path: []byte("/project/.desk-private"), Relative: []byte{}, ControlIndex: storageOwnerNoControl}
	private.Membership.ExistingObjects = []uint32{}
	// Remove the old general fixture's private marker and remap its synthetic
	// indices before this newly authored original catalog is ever encoded.
	remap := func(index uint32) uint32 {
		if index != storageOwnerNoObject && index > 19 {
			return index - 1
		}
		return index
	}
	old.Objects = append(old.Objects[:19], old.Objects[20:]...)
	for i := range old.Objects {
		old.Objects[i].Parent = remap(old.Objects[i].Parent)
	}
	for i := range old.Roots {
		row := &old.Roots[i]
		row.DirectoryObject = remap(row.DirectoryObject)
		row.DomainObject = remap(row.DomainObject)
		row.WorkObject = remap(row.WorkObject)
		if row.Source.ControlKind == 1 {
			row.Source.ControlIndex = remap(row.Source.ControlIndex)
		}
		for j, index := range row.Membership.ExistingObjects {
			row.Membership.ExistingObjects[j] = remap(index)
		}
	}
	for i := range old.OwnerControls {
		old.OwnerControls[i].ParentObject = remap(old.OwnerControls[i].ParentObject)
	}
	sort.Slice(old.Workspaces, func(i, j int) bool {
		return bytes.Compare(old.Workspaces[i].Definition.ID[:], old.Workspaces[j].Definition.ID[:]) < 0
	})
	ownerCatalogSeal(&old, &r, &cl)
	old.Sequence = 2
	old.Phase = "active"
	old.PreviousCatalogDigest = storagebudget.Digest{77}
	base, err := storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(old), r, cl)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := storageOwnerV4Origin(base, r, cl)
	if err != nil {
		t.Fatal(err)
	}
	ownerCatalogBeforeAll(&old)
	ownerCatalogSeal(&old, &r, &cl)
	b, _ := json.Marshal(old)
	var c storageOwnerV4Catalog
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	c.Version = 4
	c.Sequence = 3
	c.Phase = "prepared"
	c.Transition = storagebudget.ID{31}
	c.PreviousCatalogDigest = origin.FrameDigest
	c.TransitionKind = storageOwnerV4BridgePlan
	c.TransitionPlan = storagebudget.ID{40}
	c.UpgradeOrigin = origin
	c.LineagePlans = []storageOwnerV4LineagePlan{}
	c.LineageCompletions = []storageOwnerV4LineageCompletion{}
	c.LineageContinuations = []storageOwnerV4FromContinuation{}
	var w storageOwnerWorkspace
	for _, row := range c.Workspaces {
		if row.Definition.ID == workspaceID {
			w = row
			break
		}
	}
	privateRow := c.Roots[2]
	wd, err := storageOwnerV4WorkspaceDigest(w)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := c.old().sourceDigest(privateRow)
	if err != nil {
		t.Fatal(err)
	}
	pd, err := storageOwnerV4ProviderDigest(c.old(), w, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := identityRecord{ID: strings.Repeat("a", 64), Path: "/project", Folder: "1:2"}
	if from {
		identity.From = strings.Repeat("c", 64)
	}
	p := storageOwnerV4LineagePlan{PlanID: c.TransitionPlan, Installation: c.Installation, QBinding: c.QBinding, Workspace: storageOwnerV4Workspace{w.Definition.ID, w.Definition.Generation, wd}, PrivateRoot: storageOwnerV4PrivateRoot{privateRow.Definition.ID, privateRow.Definition.Generation, privateRow.Definition.Identity, sd}, ProjectObject: storageOwnerPersistentObject{1, [16]byte{1}, 90, 0, 1, 1}, PrivateObject: c.Objects[privateRow.DirectoryObject].Persistent, ConfigObject: r.Config, ProviderRelationDigest: pd, HistoryIdentity: append([]byte{}, w.HistoryIdentity...), AccountingOwnerKey: append([]byte{}, w.OwnerKey...), IdentityInitial: identity.line(), IdentityFromCleared: []byte{}}
	if from {
		identity.From = ""
		p.IdentityFromCleared = identity.line()
	}
	p.CompletionRoom, p.ContinuationRoom, err = storageOwnerV4Rooms(p)
	if err != nil {
		t.Fatal(err)
	}
	c.LineagePlans = append(c.LineagePlans, p)
	return c, base, r, cl
}
func ownerV4Clone(c storageOwnerV4Catalog) storageOwnerV4Catalog {
	b, _ := json.Marshal(c)
	var out storageOwnerV4Catalog
	_ = json.Unmarshal(b, &out)
	return out
}
func ownerV4Wire(t *testing.T, c storageOwnerV4Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) []byte {
	t.Helper()
	raw, err := storageOwnerV4Encode(c, r, cl)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func ownerV4Active(t *testing.T, c storageOwnerV4Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV4Catalog {
	raw := ownerV4Wire(t, c, r, cl)
	c.Sequence++
	c.Phase = "active"
	copy(c.PreviousCatalogDigest[:], raw[32:64])
	return c
}
func ownerV4Next(t *testing.T, c storageOwnerV4Catalog, kind uint8, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV4Catalog {
	raw := ownerV4Wire(t, c, r, cl)
	c = ownerV4Clone(c)
	c.Sequence++
	c.Phase = "prepared"
	c.Transition = storagebudget.ID{byte(c.Sequence + 60)}
	c.TransitionKind = kind
	copy(c.PreviousCatalogDigest[:], raw[32:64])
	return c
}
func ownerV4Receipt(n byte, identity []byte, kind uint8) storageOwnerV4Receipt {
	token := storagebudget.Token{Slot: n, ID: storagebudget.ID{n}, Sequence: uint64(n + 10), Generation: 2}
	d := sha256.Sum256(identity)
	if kind == 1 {
		d = storagebudget.Digest{}
	}
	return storageOwnerV4Receipt{OperationID: storagebudget.ID{n}, Terminal: token, PersistedApplied: token, PersistedNextSequence: uint64(n + 11), RetirementEpoch: 7, IntentDigest: storagebudget.Digest{n, 1}, TicketDigest: storagebudget.Digest{n, 2}, PublishedObject: storageOwnerPersistentObject{1, [16]byte{1}, uint64(100 + n), 0, 1, kind}, PublishedBytesDigest: d, PublicationEvidence: storagebudget.Digest{n, 3}}
}
func ownerV4Complete(t *testing.T, c storageOwnerV4Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV4Catalog {
	c = ownerV4Next(t, c, storageOwnerV4Completion, r, cl)
	p := c.LineagePlans[0]
	d, err := storageOwnerV4PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	c.LineageCompletions = append(c.LineageCompletions, storageOwnerV4LineageCompletion{p.PlanID, d, ownerV4Receipt(3, p.IdentityInitial, 2)})
	return c
}

func TestStorageOwnerCatalogV4Codec(t *testing.T) {
	c, _, r, cl := ownerV4Fixture(t, true)
	raw := ownerV4Wire(t, c, r, cl)
	if string(raw[:8]) != "DSKOWN4\x00" || binary.LittleEndian.Uint32(raw[8:12]) != 4 || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint64(raw[16:24]) != 3 {
		t.Fatal("wrong handwritten header")
	}
	decoded, err := storageOwnerV4Decode(raw, r, cl)
	if err != nil || !reflect.DeepEqual(decoded, c) {
		t.Fatal("roundtrip", err)
	}
	if _, err = storageOwnerDecodeCatalogV3(raw, r, cl); err == nil {
		t.Fatal("v3 accepted v4")
	}
	payload, _, _ := storageOwnerV4Payload(raw)
	mutations := []struct {
		name string
		body []byte
	}{
		{"unknown", bytes.Replace(payload, []byte(`"TransitionKind":5`), []byte(`"Unknown":0,"TransitionKind":5`), 1)},
		{"missing", bytes.Replace(payload, []byte(`"TransitionKind":5,`), nil, 1)},
		{"duplicate", bytes.Replace(payload, []byte(`"TransitionKind":5`), []byte(`"TransitionKind":5,"TransitionKind":5`), 1)},
		{"null-plans", bytes.Replace(payload, []byte(`"LineagePlans":`+ownerCatalogJSON(c.LineagePlans)), []byte(`"LineagePlans":null`), 1)},
		{"short-array", bytes.Replace(payload, []byte(`"PlanID":[40,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`), []byte(`"PlanID":[40]`), 1)},
		{"alternate-number", bytes.Replace(payload, []byte(`"TransitionKind":5`), []byte(`"TransitionKind":05`), 1)},
		{"whitespace", append([]byte(" "), payload...)},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			if bytes.Equal(payload, m.body) {
				t.Fatal("mutation missed operand")
			}
			bad, _ := storageOwnerV4Frame(m.body, c.Sequence)
			if _, err := storageOwnerV4Decode(bad, r, cl); err == nil {
				t.Fatal("accepted noncanonical tree")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		offset int
	}{{"version", 8}, {"header-length", 12}, {"reserved", 28}, {"checksum", 32}, {"tail-padding", storageOwnerCatalogBytes - 1}} {
		t.Run(tc.name, func(t *testing.T) {
			bad := append([]byte{}, raw...)
			bad[tc.offset] ^= 1
			if _, err := storageOwnerV4Decode(bad, r, cl); err == nil {
				t.Fatal("accepted damaged frame")
			}
		})
	}
	if _, err := storageOwnerV4Frame(make([]byte, storageOwnerCatalogBytes-63), 1); err == nil {
		t.Fatal("accepted oversized payload")
	}
	if _, err := storageOwnerV4Frame(nil, 0); err == nil {
		t.Fatal("accepted sequence zero")
	}
}

func TestStorageOwnerCatalogV4BoundsAndClosure(t *testing.T) {
	c, _, r, cl := ownerV4Fixture(t, true)
	p := c.LineagePlans[0]
	size, err := storageOwnerV4ClosureSize(c)
	if err != nil {
		t.Fatal(err)
	}
	// The reserved future tree is larger than the current tree and includes both
	// C and F even before the first actual operation has been attempted.
	body, _ := json.Marshal(c)
	if size <= len(body) {
		t.Fatal("missing future receipts")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*storageOwnerV4Catalog)
	}{
		{"underpledged-completion", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].CompletionRoom-- }},
		{"underpledged-continuation", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].ContinuationRoom-- }},
		{"overpledged-room-not-canonical", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].CompletionRoom++ }},
		{"identity-513", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].IdentityInitial = bytes.Repeat([]byte{'x'}, 513) }},
		{"nil-cleared", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].IdentityFromCleared = nil }},
		{"row-over-4096", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].HistoryIdentity = bytes.Repeat([]byte{'x'}, 4096) }},
		{"count-1025", func(c *storageOwnerV4Catalog) { c.LineagePlans = make([]storageOwnerV4LineagePlan, 1025) }},
		{"nil-table", func(c *storageOwnerV4Catalog) { c.LineageCompletions = nil }},
		{"unknown-kind", func(c *storageOwnerV4Catalog) { c.TransitionKind = 6 }},
		{"zero-plan", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].PlanID = storagebudget.ID{} }},
		{"duplicate-plan", func(c *storageOwnerV4Catalog) { c.LineagePlans = append(c.LineagePlans, c.LineagePlans[0]) }},
		{"wrong-workspace-generation", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].Workspace.Generation++ }},
		{"wrong-private-object", func(c *storageOwnerV4Catalog) { c.LineagePlans[0].PrivateObject.Object++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := ownerV4Clone(c)
			tc.mutate(&bad)
			if _, err := storageOwnerV4Encode(bad, r, cl); err == nil {
				t.Fatal("accepted bound violation")
			}
		})
	}
	// Preallocation boundaries are checked independently of identity semantics.
	for _, n := range []int{512, 513} {
		candidate := ownerV4Clone(c)
		candidate.LineagePlans[0].IdentityInitial = bytes.Repeat([]byte{'x'}, n)
		wire, _ := json.Marshal(candidate)
		if storageOwnerV4Preflight(wire) != (n == 512) {
			t.Fatal("identity preallocation boundary", n)
		}
	}
	noFrom, _, _, _ := ownerV4Fixture(t, false)
	if noFrom.LineagePlans[0].ContinuationRoom != 0 || len(noFrom.LineagePlans[0].IdentityFromCleared) != 0 {
		t.Fatal("absent From must pledge no F")
	}
	// Numeric digest width differs from actual sparse digests; rooms must reserve
	// 32 entries of 255, not the length of the eventual digest or the token alone.
	max := storageOwnerV4CompletionShadow(p)
	maxBytes, _ := json.Marshal(max)
	actual := storageOwnerV4LineageCompletion{PlanID: p.PlanID, PlanDigest: storagebudget.Digest{1}, Initial: ownerV4Receipt(1, p.IdentityInitial, 2)}
	actualBytes, _ := json.Marshal(actual)
	if uint32(len(maxBytes)) != p.CompletionRoom || len(maxBytes) <= len(actualBytes) {
		t.Fatal("not maximum encoded room")
	}
	if max.Initial.Terminal.Sequence != math.MaxUint64-1 || max.Initial.PersistedNextSequence != math.MaxUint64 {
		t.Fatal("next sequence overflow sizing")
	}
	// Counts alone cannot promise fit: a full set of large pending plans must
	// refuse its aggregate/frame closure rather than evicting any old history.
	many := ownerV4Clone(c)
	many.LineagePlans = []storageOwnerV4LineagePlan{}
	for i := 0; i < 1024; i++ {
		q := p
		q.PlanID = storagebudget.ID{byte(i >> 8), byte(i), 1}
		q.HistoryIdentity = bytes.Repeat([]byte{'h'}, 500)
		q.CompletionRoom, q.ContinuationRoom, _ = storageOwnerV4Rooms(q)
		if !storageOwnerV4PlanBounded(q) {
			t.Fatal("aggregate vector exceeds individual row cap")
		}
		many.LineagePlans = append(many.LineagePlans, q)
	}
	if _, err := storageOwnerV4ClosureSize(many); err == nil {
		t.Fatal("counts bypassed aggregate bounds")
	}
}

func TestStorageOwnerCatalogV4ReceiptsAndRetention(t *testing.T) {
	prepared, _, r, cl := ownerV4Fixture(t, true)
	active := ownerV4Active(t, prepared, r, cl)
	complete := ownerV4Complete(t, active, r, cl)
	ownerV4Wire(t, complete, r, cl)
	if !storageOwnerV4AppendOnlyKind(active, complete) {
		t.Fatal("exact C append refused")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*storageOwnerV4Catalog)
	}{
		{"missing-C", func(c *storageOwnerV4Catalog) { c.LineageCompletions = []storageOwnerV4LineageCompletion{} }},
		{"plan-mutated", func(c *storageOwnerV4Catalog) {
			c.LineagePlans[0].IdentityInitial = bytes.Replace(c.LineagePlans[0].IdentityInitial, []byte("/project"), []byte("/changed"), 1)
		}},
		{"old-row-mutated", func(c *storageOwnerV4Catalog) { c.Objects[0].FixedLength++ }},
		{"same-transition", func(c *storageOwnerV4Catalog) { c.Transition = active.Transition }},
		{"wrong-kind", func(c *storageOwnerV4Catalog) { c.TransitionKind = storageOwnerV4Plan }},
		{"wrong-plan", func(c *storageOwnerV4Catalog) { c.TransitionPlan = storagebudget.ID{77} }},
		{"upgrade-mutated", func(c *storageOwnerV4Catalog) { c.UpgradeOrigin.FrameDigest[1]++ }},
		{"extra-P", func(c *storageOwnerV4Catalog) {
			q := c.LineagePlans[0]
			q.PlanID = storagebudget.ID{90}
			c.LineagePlans = append(c.LineagePlans, q)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := ownerV4Clone(complete)
			tc.mutate(&bad)
			if storageOwnerV4AppendOnlyKind(active, bad) {
				t.Fatal("accepted illegal transition")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*storageOwnerV4Receipt)
	}{
		{"missing-operation", func(r *storageOwnerV4Receipt) { r.OperationID = storagebudget.ID{} }},
		{"missing-intent", func(r *storageOwnerV4Receipt) { r.IntentDigest = storagebudget.Digest{} }},
		{"missing-ticket", func(r *storageOwnerV4Receipt) { r.TicketDigest = storagebudget.Digest{} }},
		{"missing-native", func(r *storageOwnerV4Receipt) { r.PublishedObject = storageOwnerPersistentObject{} }},
		{"wrong-native-kind", func(r *storageOwnerV4Receipt) { r.PublishedObject.ObjectKind = 1 }},
		{"missing-bytes", func(r *storageOwnerV4Receipt) { r.PublishedBytesDigest = storagebudget.Digest{} }},
		{"missing-evidence", func(r *storageOwnerV4Receipt) { r.PublicationEvidence = storagebudget.Digest{} }},
		{"later-applied-history", func(r *storageOwnerV4Receipt) { r.PersistedApplied.Sequence++ }},
		{"different-generation", func(r *storageOwnerV4Receipt) { r.PersistedApplied.Generation++ }},
		{"different-slot", func(r *storageOwnerV4Receipt) { r.PersistedApplied.Slot++ }},
		{"next-equal", func(r *storageOwnerV4Receipt) { r.PersistedNextSequence = r.Terminal.Sequence }},
		{"missing-epoch", func(r *storageOwnerV4Receipt) { r.RetirementEpoch = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := ownerV4Clone(complete)
			tc.mutate(&bad.LineageCompletions[0].Initial)
			if _, err := storageOwnerV4Encode(bad, r, cl); err == nil {
				t.Fatal("accepted inexact receipt")
			}
		})
	}
	completed := ownerV4Active(t, complete, r, cl)
	f := ownerV4Next(t, completed, storageOwnerV4Continuation, r, cl)
	p := f.LineagePlans[0]
	d, _ := storageOwnerV4PlanDigest(p)
	source := storageOwnerPersistentObject{1, [16]byte{1}, 300, 0, 1, 1}
	target := source
	target.Object = 301
	moved := storageOwnerV4FromContinuation{PlanID: p.PlanID, PlanDigest: d, StampDisposition: storageOwnerV4Moved, StampSourceBefore: source, StampTargetAfter: source, StampReceipt: ownerV4Receipt(4, nil, 1), Rewrite: ownerV4Receipt(5, p.IdentityFromCleared, 2)}
	moved.StampReceipt.PublishedObject = source
	both := moved
	both.StampDisposition = storageOwnerV4BothPresent
	both.StampTargetBefore = target
	both.StampSourceAfter = source
	both.StampTargetAfter = target
	both.StampReceipt = storageOwnerV4Receipt{}
	absent := moved
	absent.StampDisposition = storageOwnerV4SourceAbsent
	absent.StampSourceBefore = storageOwnerPersistentObject{}
	absent.StampTargetBefore = target
	absent.StampTargetAfter = target
	absent.StampReceipt = storageOwnerV4Receipt{}
	for _, row := range []storageOwnerV4FromContinuation{moved, both, absent} {
		candidate := ownerV4Clone(f)
		candidate.LineageContinuations = []storageOwnerV4FromContinuation{row}
		ownerV4Wire(t, candidate, r, cl)
		if !storageOwnerV4AppendOnlyKind(completed, candidate) {
			t.Fatal("valid F append refused")
		}
	}
	for _, tc := range []struct {
		name   string
		row    storageOwnerV4FromContinuation
		mutate func(*storageOwnerV4FromContinuation)
	}{
		{"unknown-disposition", moved, func(f *storageOwnerV4FromContinuation) { f.StampDisposition = 4 }},
		{"move-target-present", moved, func(f *storageOwnerV4FromContinuation) { f.StampTargetBefore = target }},
		{"move-lost-object", moved, func(f *storageOwnerV4FromContinuation) { f.StampTargetAfter = target }},
		{"move-missing-receipt", moved, func(f *storageOwnerV4FromContinuation) { f.StampReceipt = storageOwnerV4Receipt{} }},
		{"both-fabricated-receipt", both, func(f *storageOwnerV4FromContinuation) { f.StampReceipt = ownerV4Receipt(4, nil, 1) }},
		{"absent-source-present", absent, func(f *storageOwnerV4FromContinuation) { f.StampSourceBefore = source }},
		{"wrong-cleared-bytes", moved, func(f *storageOwnerV4FromContinuation) {
			f.Rewrite.PublishedBytesDigest = sha256.Sum256(p.IdentityInitial)
		}},
		{"same-attempt", moved, func(f *storageOwnerV4FromContinuation) { f.Rewrite.OperationID = f.StampReceipt.OperationID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := tc.row
			tc.mutate(&row)
			bad := ownerV4Clone(f)
			bad.LineageContinuations = []storageOwnerV4FromContinuation{row}
			if _, err := storageOwnerV4Encode(bad, r, cl); err == nil {
				t.Fatal("accepted invalid F")
			}
		})
	}
	// Enrollment retains history but appends no P/C/F. Its active entrypoints
	// remain unselected; this only checks the closed kind's data contract.
	enrollment := ownerV4Next(t, completed, storageOwnerV4Enrollment, r, cl)
	enrollment.TransitionPlan = storagebudget.ID{}
	ownerV4Wire(t, enrollment, r, cl)
	if !storageOwnerV4AppendOnlyKind(completed, enrollment) {
		t.Fatal("retained enrollment")
	}
	plan := ownerV4Next(t, completed, storageOwnerV4Plan, r, cl)
	q := plan.LineagePlans[0]
	q.PlanID = storagebudget.ID{41}
	q.CompletionRoom, q.ContinuationRoom, _ = storageOwnerV4Rooms(q)
	plan.LineagePlans = append(plan.LineagePlans, q)
	plan.TransitionPlan = q.PlanID
	ownerV4Wire(t, plan, r, cl)
	if !storageOwnerV4AppendOnlyKind(completed, plan) {
		t.Fatal("new plan append")
	}
}

func TestStorageOwnerCatalogV4Bridge(t *testing.T) {
	p, base, r, cl := ownerV4Fixture(t, false)
	prepared := ownerV4Wire(t, p, r, cl)
	active := ownerV4Active(t, p, r, cl)
	activeRaw := ownerV4Wire(t, active, r, cl)
	blank := make([]byte, storageOwnerCatalogBytes)
	torn := append([]byte{}, activeRaw...)
	torn[100] ^= 1
	cases := []struct {
		name  string
		a, b  []byte
		class storageOwnerV4BridgeClass
	}{
		{"old-identical", base, base, storageOwnerV4OldPair},
		{"mixed", base, prepared, storageOwnerV4BridgeNeedsDuplicate},
		{"mixed-reversed", prepared, base, storageOwnerV4BridgeNeedsDuplicate},
		{"prepared-blank", prepared, blank, storageOwnerV4PreparedProvisionalBarrierUnproved},
		{"prepared-duplicated", prepared, prepared, storageOwnerV4PreparedDuplicatedProvisional},
		{"prepared-active", prepared, activeRaw, storageOwnerV4AdjacentProvisional},
		{"active-blank", activeRaw, blank, storageOwnerV4ActiveNeedsExactPeer},
		{"active-torn-recognized", activeRaw, torn, storageOwnerV4ActiveNeedsExactPeer},
		{"active-duplicated", activeRaw, activeRaw, storageOwnerV4ActiveDuplicatedProvisional},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageOwnerV4Classify(tc.a, tc.b, r, cl)
			if err != nil || got.Class != tc.class {
				t.Fatalf("class %v err %v", got.Class, err)
			}
		})
	}
	completed := ownerV4Complete(t, active, r, cl)
	completedRaw := ownerV4Wire(t, completed, r, cl)
	if got, err := storageOwnerV4Classify(activeRaw, completedRaw, r, cl); err != nil || got.Class != storageOwnerV4AdjacentProvisional {
		t.Fatal("adjacent new C", got.Class, err)
	}
	badOrigin := ownerV4Clone(p)
	badOrigin.UpgradeOrigin.FrameDigest[0] ^= 1
	badOriginBody, _ := json.Marshal(badOrigin)
	badOriginRaw, _ := storageOwnerV4Frame(badOriginBody, badOrigin.Sequence)
	if _, err := storageOwnerV4Encode(badOrigin, r, cl); err == nil {
		t.Fatal("encoded inconsistent bridge predecessor")
	}
	if _, err := storageOwnerV4Decode(badOriginRaw, r, cl); err == nil {
		t.Fatal("decoded inconsistent bridge predecessor")
	}
	equalConflict := ownerV4Clone(active)
	equalConflict.Transition = storagebudget.ID{99}
	conflictRaw := ownerV4Wire(t, equalConflict, r, cl)
	gap := ownerV4Clone(completed)
	gap.Sequence++
	gapRaw := ownerV4Wire(t, gap, r, cl)
	for _, tc := range []struct {
		name string
		a, b []byte
	}{
		{"no-valid-v4", blank, blank}, {"recognized-malformed-with-old", base, torn},
		{"unrelated-origin", base, badOriginRaw}, {"bad-origin-single", badOriginRaw, blank}, {"bad-origin-duplicate", badOriginRaw, badOriginRaw}, {"old-with-completion", base, completedRaw},
		{"equal-sequence-conflict", activeRaw, conflictRaw}, {"broken-adjacency", activeRaw, gapRaw},
		{"missing-original-bank", activeRaw, nil},
		{"ordinary-prepared-duplicate", completedRaw, completedRaw},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := storageOwnerV4Classify(tc.a, tc.b, r, cl); err == nil {
				t.Fatal("accepted invalid pair")
			}
		})
	}
	// Saved factual C/F survive exactly the crash cut where publishing Active
	// into the opposite original bank tears. No Q/J slot is supplied or recaptured.
	// F requires a plan with From, so author that separate original lineage.
	fromP, _, fromR, fromCl := ownerV4Fixture(t, true)
	fromActive := ownerV4Active(t, fromP, fromR, fromCl)
	fromC := ownerV4Complete(t, fromActive, fromR, fromCl)
	fromCActive := ownerV4Active(t, fromC, fromR, fromCl)
	f := ownerV4Next(t, fromCActive, storageOwnerV4Continuation, fromR, fromCl)
	plan := f.LineagePlans[0]
	pd, _ := storageOwnerV4PlanDigest(plan)
	f.LineageContinuations = []storageOwnerV4FromContinuation{{PlanID: plan.PlanID, PlanDigest: pd, StampDisposition: storageOwnerV4SourceAbsent, Rewrite: ownerV4Receipt(5, plan.IdentityFromCleared, 2)}}
	for _, tc := range []struct {
		name    string
		catalog storageOwnerV4Catalog
		record  storageOwnerRecordV2
		claim   storageOwnerClaimV2
	}{
		{"saved-C", completed, r, cl}, {"saved-F", f, fromR, fromCl},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := ownerV4Wire(t, tc.catalog, tc.record, tc.claim)
			final := ownerV4Active(t, tc.catalog, tc.record, tc.claim)
			finalRaw := ownerV4Wire(t, final, tc.record, tc.claim)
			damaged := append([]byte{}, finalRaw...)
			damaged[100] ^= 1
			for _, cut := range []struct {
				name string
				peer []byte
			}{{"torn-active", damaged}, {"blank-peer", blank}} {
				t.Run(cut.name, func(t *testing.T) {
					got, err := storageOwnerV4Classify(saved, cut.peer, tc.record, tc.claim)
					if err != nil || got.Class != storageOwnerV4SavedPreparedReceiptProvisional || got.Selected != 0 || !reflect.DeepEqual(got.Catalog, tc.catalog) {
						t.Fatal("lost immutable saved receipt", got.Class, err)
					}
					reverse, err := storageOwnerV4Classify(cut.peer, saved, tc.record, tc.claim)
					if err != nil || reverse.Class != storageOwnerV4SavedPreparedReceiptProvisional || reverse.Selected != 1 || !reflect.DeepEqual(reverse.Catalog, tc.catalog) {
						t.Fatal("reversed saved receipt pair", err)
					}
				})
			}
			t.Run("malformed-saved-receipt", func(t *testing.T) {
				bad := ownerV4Clone(tc.catalog)
				if bad.TransitionKind == storageOwnerV4Completion {
					bad.LineageCompletions[0].Initial.PersistedApplied.Sequence++
				} else {
					bad.LineageContinuations[0].Rewrite.OperationID = storagebudget.ID{}
				}
				body, _ := json.Marshal(bad)
				badRaw, _ := storageOwnerV4Frame(body, bad.Sequence)
				if _, err := storageOwnerV4Classify(badRaw, damaged, tc.record, tc.claim); err == nil {
					t.Fatal("malformed saved factual row accepted")
				}
			})
			// The header/payload checksum excludes tail padding, but includes
			// reserved byte28. Author both exact checksum-valid malformed
			// envelopes; full Payload refusal must not make either look torn.
			reserved := append([]byte{}, finalRaw...)
			reserved[28] = 1
			end := 64 + int(binary.LittleEndian.Uint32(reserved[24:28]))
			h := sha256.New()
			h.Write(reserved[:32])
			h.Write(reserved[64:end])
			copy(reserved[32:64], h.Sum(nil))
			padding := append([]byte{}, finalRaw...)
			padding[len(padding)-1] = 1
			for _, peer := range [][]byte{reserved, padding} {
				if !storageOwnerV4RecognizedChecksum(peer) {
					t.Fatal("malformed envelope vector lost declared checksum")
				}
				if _, _, err := storageOwnerV4Payload(peer); err == nil {
					t.Fatal("malformed envelope vector parsed successfully")
				}
			}
			if storageOwnerV4RecognizedChecksum(damaged) || storageOwnerV4RecognizedChecksum(blank) {
				t.Fatal("torn/blank positive confused with checksum-valid envelope")
			}
			oversizedLength := append([]byte{}, finalRaw...)
			binary.LittleEndian.PutUint32(oversizedLength[24:28], storageOwnerCatalogBytes-63)
			if storageOwnerV4RecognizedChecksum(oversizedLength) || storageOwnerV4RecognizedChecksum(finalRaw[:63]) {
				t.Fatal("unbounded checksum slicing operand accepted")
			}
			conflict := ownerV4Clone(tc.catalog)
			conflict.Transition = storagebudget.ID{127}
			conflictRaw := ownerV4Wire(t, conflict, tc.record, tc.claim)
			malformed := ownerV4Clone(tc.catalog)
			malformed.TransitionKind = 6
			body, _ := json.Marshal(malformed)
			malformedRaw, _ := storageOwnerV4Frame(body, malformed.Sequence)
			for _, bad := range []struct {
				name string
				peer []byte
			}{{"conflicting-valid", conflictRaw}, {"malformed-intact", malformedRaw}, {"reserved-header-checksummed", reserved}, {"nonzero-padding-checksummed", padding}, {"missing-peer", nil}, {"duplicate-ordinary-prepared", saved}} {
				t.Run(bad.name, func(t *testing.T) {
					if _, err := storageOwnerV4Classify(saved, bad.peer, tc.record, tc.claim); err == nil {
						t.Fatal("ambiguous saved receipt pair accepted")
					}
					if _, err := storageOwnerV4Classify(bad.peer, saved, tc.record, tc.claim); err == nil {
						t.Fatal("reversed malformed/conflicting peer accepted")
					}
				})
			}
		})
	}
	got, err := storageOwnerV4Classify(base, torn, r, cl)
	if err == nil || got.Class != storageOwnerV4MalformedRecognized {
		t.Fatal("recognized v4 fell back to v3")
	}
	// A raw duplicate makes no additional sequence/row/receipt claim.
	got, _ = storageOwnerV4Classify(activeRaw, activeRaw, r, cl)
	if !reflect.DeepEqual(got.Catalog, active) || len(got.Catalog.LineageCompletions) != 0 {
		t.Fatal("manufactured history")
	}
}

func TestStorageOwnerCatalogV4Effects(t *testing.T) {
	expected := []string{
		"lin4/plan-prepared/publish", "lin4/plan-prepared/persist", "lin4/plan-active/publish", "lin4/plan-active/persist",
		"lin4/completion-prepared/publish", "lin4/completion-prepared/persist", "lin4/completion-active/publish", "lin4/completion-active/persist",
		"lin4/from-prepared/publish", "lin4/from-prepared/persist", "lin4/from-active/publish", "lin4/from-active/persist",
		"lin4/bridge-duplicate/publish", "lin4/bridge-duplicate/persist", "lin4/reopen-base/persist"}
	var total, policy, inodes uint64
	publications, persistences := 0, 0
	rows := storageOwnerV4CatalogEffects()
	for i, row := range rows {
		if row.Key != expected[i] || row.Bytes != 8388608 || row.Files != 0 || row.Directories != 0 || row.Metadata != 4 || row.PolicyBytes != 17301504 || row.PolicyInodes != 2 || row.MaxPerLaunch != 1 || row.Parent != storageOwnerV4OriginalConfigParent {
			t.Fatal("effect differs from reviewed finite table", i, row)
		}
		if row.Action == "publish" {
			publications++
		} else if row.Action == "persist" {
			persistences++
		} else {
			t.Fatal("unknown action")
		}
		if _, ok := storageOwnerBootstrapEffects[row.Key]; ok {
			t.Fatal("alias of ordinary enrollment")
		}
		demand, err := storageAdvisoryEstimate(row.Bytes, row.Files, row.Directories, row.Metadata)
		if err != nil || demand.Bytes != row.PolicyBytes || demand.Inodes != row.PolicyInodes {
			t.Fatal("estimator disagreement", err)
		}
		total += row.Bytes
		policy += row.PolicyBytes
		inodes += row.PolicyInodes
	}
	if publications != 7 || persistences != 8 || total != 125829120 || policy != 259522560 || inodes != 30 {
		t.Fatal("wrong whole-launch ceiling")
	}
	rows[0].Key = "corrupted"
	if storageOwnerV4CatalogEffects()[0].Key != expected[0] {
		t.Fatal("caller mutated table")
	}
}

func TestStorageOwnerCatalogV4Digests(t *testing.T) {
	// Numeric preimages/answers are the reviewed handwritten D1/D2/D8 examples.
	limits := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 1, Files: 1, Directories: 1}}
	for i := range limits.Category {
		limits.Category[i] = limits.Total
	}
	w := storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: storagebudget.ID{1}, Generation: 1, Limits: limits}, OwnerKind: 3, OwnerKey: []byte(strings.Repeat("a", 64)), HistoryIdentity: []byte("/example/project")}
	d, err := storageOwnerV4WorkspaceDigest(w)
	if err != nil || hex.EncodeToString(d[:]) != "27fb2b4b8a86a3c8c4461b394fcf0d519af6b402ba25139076956a63c818ce45" {
		t.Fatal("D2 vector", d, err)
	}
	absentScope := storageOwnerCatalogV2{Installation: storagebudget.ID{2}, QBinding: storagebudget.Digest{3}}
	d, err = storageOwnerV4ProviderDigest(absentScope, w, nil)
	if err != nil || hex.EncodeToString(d[:]) != "4111eef9009a887d1e6480b153997f6fd44bb204cc492401824ca9094c779711" {
		t.Fatal("D1 absent vector", d, err)
	}
	other := w
	other.Definition.ID = storagebudget.ID{2}
	otherAbsent, _ := storageOwnerV4ProviderDigest(absentScope, other, nil)
	if otherAbsent == d {
		t.Fatal("absent sentinel lost workspace scope")
	}
	identity := []byte(`{"id":"` + strings.Repeat("b", 64) + `","path":"/example/project"}` + "\n")
	d, err = storageOwnerV4IdentityDigest(identity)
	if err != nil || hex.EncodeToString(d[:]) != "0c4b349133e0ee7bc262edbc1b5b764512719e67eb1ffa4a9ff0ddc0335b8287" {
		t.Fatal("D8 vector", d, err)
	}
	if _, err = storageOwnerV4IdentityDigest(identity[:len(identity)-1]); err == nil {
		t.Fatal("D8 missing LF")
	}
	if _, err = storageOwnerV4IdentityDigest([]byte{}); err == nil {
		t.Fatal("D8 empty sentinel")
	}
	c, base, r, cl := ownerV4Fixture(t, false)
	p := c.LineagePlans[0]
	actual, err := storageOwnerV4PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	q := p
	q.CompletionRoom--
	changed, err := storageOwnerV4PlanDigest(q)
	if err != nil || changed == actual {
		t.Fatal("D3 excluded pledge")
	}
	operation, err := storageOwnerV4Operation(actual, "create")
	if err != nil || operation != "p:"+hex.EncodeToString(actual[:])+":create" || len(operation) > 128 {
		t.Fatal("attempt mapping")
	}
	for _, action := range []string{"stamp", "clear"} {
		t.Run(action, func(t *testing.T) {
			if _, err := storageOwnerV4Operation(actual, action); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := storageOwnerV4Operation(actual, "retry"); err == nil {
		t.Fatal("unknown action")
	}
	origin, err := storageOwnerV4Origin(base, r, cl)
	if err != nil || !bytes.Equal(origin.FrameDigest[:], base[32:64]) || origin.FrameDigest == sha256.Sum256(base) {
		t.Fatal("D5 wrong operand")
	}
	old, err := storageOwnerDecodeCatalogV3(base, r, cl)
	if err != nil {
		t.Fatal(err)
	}
	d, err = storageOwnerV4OldDigest(storageOwnerCatalogV2(old))
	if err != nil || d != c.UpgradeOrigin.OldProjectionDigest {
		t.Fatal("D4 source", err)
	}
	d2, err := storageOwnerV4OldDigest(c.old())
	if err != nil || d2 != d {
		t.Fatal("D4 included envelope", err)
	}
	changedOld := c.old()
	changedOld = ownerCatalogClone(changedOld)
	changedOld.OwnerControls[0].Location.Path = []byte("/changed")
	d3, err := storageOwnerV4OldDigest(changedOld)
	if err != nil || d3 == d {
		t.Fatal("D4 omitted full Location")
	}
	provider := c.Roots[4]
	var providerWorkspace, standaloneWorkspace storageOwnerWorkspace
	for _, row := range c.Workspaces {
		if row.Definition.ID == (storagebudget.ID{2}) {
			providerWorkspace = row
		} else {
			standaloneWorkspace = row
		}
	}
	providerDigest, err := storageOwnerV4ProviderDigest(c.old(), providerWorkspace, &provider)
	if err != nil || providerDigest == (storagebudget.Digest{}) {
		t.Fatal("D1 present", err)
	}
	if _, err := storageOwnerV4ProviderDigest(c.old(), standaloneWorkspace, &provider); err == nil {
		t.Fatal("unlinked provider accepted")
	}
	changedProvider := provider
	changedProvider.Source.ControlIndex = 999999
	if _, err := storageOwnerV4ProviderDigest(c.old(), providerWorkspace, &changedProvider); err == nil {
		t.Fatal("invalid marker index")
	}
	// D6's complete-page outer hash includes the already populated checksum.
	// This is a synthetic authored fixture, not evidence of original selection.
	intent := storageIntentTestRecord()
	installation, binding := storagebudget.ID{70}, storagebudget.Digest{71}
	page, err := storageEncodeIntent(intent, installation, binding)
	if err != nil {
		t.Fatal(err)
	}
	d, err = storageOwnerV4IntentDigest(page, 63, installation, binding, intent)
	if err != nil || d != sha256.Sum256(page) {
		t.Fatal("D6 full-page commitment", err)
	}
	noChecksum := append([]byte{}, page...)
	clear(noChecksum[76:108])
	if d == sha256.Sum256(noChecksum) {
		t.Fatal("D6 checksum excluded")
	}
	if _, err := storageOwnerV4IntentDigest(page[:16383], 63, installation, binding, intent); err == nil {
		t.Fatal("truncated intent")
	}
	wrong := intent
	wrong.OperationID = storagebudget.ID{99}
	if _, err := storageOwnerV4IntentDigest(page, 63, installation, binding, wrong); err == nil {
		t.Fatal("D6 wrong original record")
	}
	ticket := storagebudget.Ticket{Token: intent.Token, State: storagebudget.Terminal, PolicyRevision: 5, Workflow: "desk-signing-lineage-v1", Operation: operation, Owner: binding, Manifest: storagebudget.Digest{88}, ManifestLength: storagebudget.ManifestBytes, Components: intent.Components, Changes: intent.Changes, Evidence: intent.Evidence}
	td, err := storageOwnerV4TicketDigest(installation, binding, ticket)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*storagebudget.Ticket){
		func(t *storagebudget.Ticket) { t.Evidence[1]++ },
		func(t *storagebudget.Ticket) { t.Components[0].Capacity[3].Peak.Inodes-- },
		func(t *storagebudget.Ticket) { t.Changes[0].AttributionRemove.Bytes-- },
	} {
		b, _ := json.Marshal(ticket)
		var changed storagebudget.Ticket
		_ = json.Unmarshal(b, &changed)
		mutate(&changed)
		got, err := storageOwnerV4TicketDigest(installation, binding, changed)
		if err != nil || got == td {
			t.Fatal("D7 omitted complete ticket field", err)
		}
	}
	ticket.State = storagebudget.Reserved
	if _, err := storageOwnerV4TicketDigest(installation, binding, ticket); err == nil {
		t.Fatal("nonterminal D7")
	}
}

func TestStorageOwnerCatalogV4StandalonePrivateJoin(t *testing.T) {
	c, _, r, cl := ownerV4Fixture(t, false)
	p := c.LineagePlans[0]
	row := c.Roots[2]
	if row.Role != 6 || row.Category != storagebudget.Control || row.Source.ParentRoot != (storagebudget.ID{}) || string(row.Source.Path) != "/project/.desk-private" || len(row.Membership.ExistingObjects) != 0 {
		t.Fatal("positive fixture is not source-shaped standalone Control")
	}
	for _, object := range c.Objects {
		if object.Parent == row.DirectoryObject {
			t.Fatal("original private fixture is not empty")
		}
	}
	ownerV4Wire(t, c, r, cl)
	for _, tc := range []struct {
		name   string
		mutate func(*storageOwnerCatalogV2)
	}{
		{"managed-role3", func(old *storageOwnerCatalogV2) {
			row := &old.Roots[2]
			row.Role = 3
			row.Source.Kind = 3
			row.Source.OwnerKey = []byte(strings.Repeat("c", 32))
		}},
		{"wrong-source-key", func(old *storageOwnerCatalogV2) { old.Roots[2].Source.OwnerKey = []byte(strings.Repeat("d", 64)) }},
		{"wrong-role-history", func(old *storageOwnerCatalogV2) {
			row := &old.Roots[2]
			row.Role = 4
			row.Source.Kind = 4
			row.Category = storagebudget.History
		}},
		{"retained-role6", func(old *storageOwnerCatalogV2) { old.Roots[2].Category = storagebudget.Retained }},
		{"unrelated-control-role6", func(old *storageOwnerCatalogV2) { old.Roots[2].Source.Path = []byte("/project/.other-private") }},
		{"wrong-workspace-kind", func(old *storageOwnerCatalogV2) {
			for i := range old.Workspaces {
				if old.Workspaces[i].Definition.ID == p.Workspace.ID {
					old.Workspaces[i].OwnerKind = 1
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := ownerV4Clone(c)
			old := candidate.old()
			tc.mutate(&old)
			record, claim := r, cl
			ownerCatalogSeal(&old, &record, &claim)
			if err := old.validateVersion(record, claim, 3); err != nil {
				t.Fatal("counterexample must retain valid original v3 rows", err)
			}
			body, _ := json.Marshal(old)
			if err := json.Unmarshal(body, &candidate); err != nil {
				t.Fatal(err)
			}
			candidate.Version = 4
			plan := candidate.LineagePlans[0]
			private := old.Roots[2]
			plan.PrivateRoot.Identity = private.Definition.Identity
			plan.PrivateRoot.SourceDigest, _ = old.sourceDigest(private)
			for _, workspace := range old.Workspaces {
				if workspace.Definition.ID == plan.Workspace.ID {
					plan.Workspace.Digest, _ = storageOwnerV4WorkspaceDigest(workspace)
					plan.ProviderRelationDigest, _ = storageOwnerV4ProviderDigest(old, workspace, nil)
				}
			}
			// Refreshing exact digests cannot turn an unrelated original source into
			// the narrow standalone private/workspace/key relation.
			if err := candidate.validatePlan(plan, record); err == nil {
				t.Fatal("unrelated valid original row admitted")
			}
		})
	}
}
