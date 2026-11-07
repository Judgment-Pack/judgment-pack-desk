package desk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageTransferPriorControlProjectionIsExactAndDetached(t *testing.T) {
	native := storageTransferVector().Source
	dict := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}, Objects: []storageOwnerObject{}, OwnerControls: []storageOwnerControl{}, Roots: []storageOwnerRoot{}}
	for i := 0; i < 4; i++ {
		n := native
		n.Object = uint64(i + 1)
		dict.Objects = append(dict.Objects, storageOwnerObject{Persistent: native, Parent: storageOwnerNoObject})
		dict.Objects[i].Persistent = n
	}
	for i, path := range []string{"/before", "/older", "/after"} {
		dict.Roots = append(dict.Roots, storageOwnerRoot{Role: 4, DirectoryObject: uint32(i + 1), DomainObject: storageOwnerNoObject, WorkObject: storageOwnerNoObject, Source: storageOwnerRootSource{Kind: 4, Path: []byte(path), ControlKind: 2, ControlIndex: 0, ControlSelection: byte([]int{2, 3, 1}[i])}})
	}
	dict.OwnerControls = append(dict.OwnerControls, storageOwnerControl{Role: 2, ParentObject: 0, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte("original"), Location: storageOwnerLocation{Version: 1, Path: []byte("/after"), Previous: []byte("/before"), Selected: dict.Objects[3].Persistent, PreviousSelected: dict.Objects[1].Persistent}, MaxBytes: 16384})
	prior := storageEnrollmentPeakProjection{Catalog: storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}}, TransferPrior: &storageEnrollmentTransferPriorControl{Controls: []storageEnrollmentTransferPriorLocation{{Control: 0, Selected: 0, Previous: 1}}}}
	prior.Select[0] = []uint16{0, 1, 2}
	prior.Select[1] = []uint16{0}
	prior.Select[2] = []uint16{0, 1}
	for i := 3; i < len(prior.Select); i++ {
		prior.Select[i] = []uint16{}
	}
	before, err := storageEnrollmentPeakProjectionExpand(prior, dict)
	if err != nil {
		t.Fatal(err)
	}
	control := before.OwnerControls[0]
	if !bytes.Equal(control.Location.Path, []byte("/before")) || !bytes.Equal(control.Location.Previous, []byte("/older")) || control.Location.Selected != dict.Objects[1].Persistent || control.Location.PreviousSelected != dict.Objects[2].Persistent || before.Roots[0].Source.ControlSelection != 1 || before.Roots[1].Source.ControlSelection != 2 {
		t.Fatal("wrong original projection", control.Location)
	}
	digest, err := before.controlDigest(control)
	if err != nil || digest != control.RelationDigest {
		t.Fatal("old complete relation digest lost", err)
	}
	if string(dict.OwnerControls[0].Location.Path) != "/after" || dict.Roots[0].Source.ControlSelection != 2 || dict.Roots[1].Source.ControlSelection != 3 {
		t.Fatal("projection mutated caller")
	}
	before.OwnerControls[0].Location.Path[1] = 'x'
	if string(dict.Roots[0].Source.Path) != "/before" {
		t.Fatal("old path slice aliased")
	}
	for _, bad := range []storageEnrollmentTransferPriorLocation{{Control: 1, Selected: 0, Previous: 1}, {Control: 0, Selected: 2, Previous: 1}, {Control: 0, Selected: 0, Previous: 0}, {Control: 0, Selected: 0, Previous: 2}} {
		copy := prior
		copy.TransferPrior = &storageEnrollmentTransferPriorControl{Controls: []storageEnrollmentTransferPriorLocation{bad}}
		if _, err := storageEnrollmentPeakProjectionExpand(copy, dict); err == nil {
			t.Fatal("invalid prior indices accepted", bad)
		}
	}
	absent := prior
	absent.TransferPrior = &storageEnrollmentTransferPriorControl{Controls: []storageEnrollmentTransferPriorLocation{{Control: 0, Selected: 0, Previous: 65535}}}
	expanded, err := storageEnrollmentPeakProjectionExpand(absent, dict)
	if err != nil || len(expanded.OwnerControls[0].Location.Previous) != 0 || expanded.OwnerControls[0].Location.PreviousSelected != (storageOwnerPersistentObject{}) {
		t.Fatal("positive no-Previous projection changed", err)
	}
}

func TestStorageTransferProjectionClosedMaximum(t *testing.T) {
	// Handwritten field arithmetic. Objects+controls share8192, not8192 each.
	// Header/policy792; dictionary counts16; dictionary indices30080;
	// before-ID counts8+92160; OriginalRef2; saved outer length2+8320.
	legacyMaximum := 792 + 16 + 30080 + 8 + 92160 + 2 + 2 + 8320
	transferMaximum := legacyMaximum - 5760*14 + 16 + 8192*8 + 8192 + 512
	if legacyMaximum != 131380 || transferMaximum != 124996 || transferMaximum > storageEnrollmentPeakProjectionMax || storageEnrollmentPeakProjectionMax != 135168 {
		t.Fatal(legacyMaximum, transferMaximum, storageEnrollmentPeakProjectionMax)
	}
}

func TestStorageTransferProjectionRetainsExactOldTargetReferences(t *testing.T) {
	native := storageTransferVector().Source
	a, b, c := storagebudget.ID{1}, storagebudget.ID{2}, storagebudget.ID{3}
	dict := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}, Objects: []storageOwnerObject{{Persistent: native, Parent: storageOwnerNoObject}}, Roots: []storageOwnerRoot{{Role: 4, DirectoryObject: 0, DomainObject: storageOwnerNoObject, WorkObject: storageOwnerNoObject, Source: storageOwnerRootSource{ControlIndex: storageOwnerNoControl}, References: []storagebudget.ID{a, b, c}}}, Workspaces: []storageOwnerWorkspace{}}
	for _, id := range []storagebudget.ID{a, b, c} {
		dict.Workspaces = append(dict.Workspaces, storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: id, Generation: 7}})
	}
	prior := storageEnrollmentPeakProjection{Catalog: storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}}, TransferPrior: &storageEnrollmentTransferPriorControl{References: [8192]byte{6}}}
	prior.Select[0] = []uint16{0}
	prior.Select[2] = []uint16{0}
	prior.Select[3] = []uint16{0, 1, 2}
	old, err := storageEnrollmentPeakProjectionExpand(prior, dict)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Roots[0].References) != 2 || old.Roots[0].References[0] != b || old.Roots[0].References[1] != c || old.Workspaces[2].Definition.Generation != 7 {
		t.Fatal("old target membership/generation lost")
	}
	old.Roots[0].References[0] = a
	if dict.Roots[0].References[0] != a || dict.Roots[0].References[1] != b {
		t.Fatal("membership aliased")
	}
	for _, bits := range [][8192]byte{{8}, {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 128}} {
		bad := *prior.TransferPrior
		bad.References = bits
		copy := prior
		copy.TransferPrior = &bad
		if _, err := storageEnrollmentPeakProjectionExpand(copy, dict); err == nil {
			t.Fatal("unchanged or missing workspace accepted")
		}
	}
	// Empty B is represented exactly, without attributing new source members to
	// the original before image. It remains distinct from absent extension.
	empty := *prior.TransferPrior
	empty.References = [8192]byte{}
	prior.TransferPrior = &empty
	old, err = storageEnrollmentPeakProjectionExpand(prior, dict)
	if err != nil || len(old.Roots[0].References) != 0 {
		t.Fatal("empty original membership lost", err)
	}
}

func TestStorageTransferPPathEncodingPreservesLegacyLimit(t *testing.T) {
	catalog, _, _ := ownerCatalogFixture()
	catalog.Version = 1
	for _, n := range []int{4096, 4097, 16384, 16385} {
		copy := ownerCatalogClone(catalog)
		copy.Roots[0].Source.Path = []byte("/" + strings.Repeat("a", n-1))
		raw, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		var decoded storageOwnerCatalogV2
		err = storageEnrollmentPeakCatalogJSON(raw, &decoded)
		if (err == nil) != (n <= 16384) || storageEnrollmentPeakCatalogBounded(copy) != (n <= 16384) {
			t.Fatal("P path boundary", n, err)
		}
		// Legacy entry points retain4096 even for these detached Version1 bytes.
		if (storageOwnerCatalogJSON(raw, &decoded) == nil) != (n <= 4096) {
			t.Fatal("legacy scanner changed", n)
		}
		if n <= 16384 {
			packed, err := storageEnrollmentPeakPackInner(raw)
			if err != nil {
				t.Fatal(n, err)
			}
			restored, err := storageEnrollmentPeakUnpackInner(packed)
			if err != nil || !bytes.Equal(restored, raw) {
				t.Fatal("P packed path lost", n, err)
			}
		}
		copy.Version = 3
		legacy, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		if (storageEnrollmentPeakCatalogJSON(legacy, &decoded) == nil) != (n <= 4096) {
			t.Fatal("V3 path widened", n)
		}
	}
}

func TestStorageTransferSourceDiscriminatorAndPathBounds(t *testing.T) {
	_, _, base, _, _ := epCatalogs(t)
	for _, workflow := range []byte{4, 5, 6} {
		for _, n := range []int{4096, 4097, 16384, 16385} {
			source := base
			source.Namespace = []byte("transfer-base")
			source.Default = &storageEnrollmentDefaultSource{Workflow: workflow, Branch: storageCreationConfigured, Settings: base.Project, Selected: base.Project, SelectedPath: []byte("/" + strings.Repeat("a", n-1)), Next: []byte("next"), ChainCount: 1, ChainDigest: storagebudget.Digest{41}, SourceNative: base.Project}
			if workflow == 5 {
				source.Namespace = []byte("transfer-data")
				source.Default.Next = []byte{}
				source.Default.Marker = epNative(301, 2)
				source.Default.Location = epNative(302, 2)
				source.Default.LocationPayload = storagebudget.Digest{17}
				source.Default.CreationPage = storagebudget.Digest{18}
			}
			if workflow == 6 {
				source.Namespace = []byte("transfer-root")
				source.Default.OwnSelf = 1
				source.Default.Next = []byte{}
			}
			raw, err := storageEnrollmentPeakEncodeSource(source)
			if (err == nil) != (n <= 16384) {
				t.Fatal("transfer source bound", workflow, n, err)
			}
			if err == nil {
				decoded, err := storageEnrollmentPeakDecodeSource(raw)
				if err != nil || len(decoded.Default.SelectedPath) != n || decoded.Default.Workflow != workflow {
					t.Fatal("source discriminator/path lost", err)
				}
			}
			// Old closed workflow1/2/3 cannot be relabeled into this namespace or inherit
			// its wider bound. These vectors never supply original qualification.
			for _, old := range []byte{1, 2, 3} {
				copy := source
				d := *source.Default
				copy.Default = &d
				d.Workflow = old
				if _, err := storageEnrollmentPeakEncodeSource(copy); err == nil {
					t.Fatal("transfer source relabeled as old", old)
				}
			}
		}
	}
}

func TestStorageTransferSourceClosedMaximumAndLegacyFamilies(t *testing.T) {
	_, _, base, _, _ := epCatalogs(t)
	source := base
	source.OwnerKey = bytes.Repeat([]byte{'o'}, 4096)
	source.HistoryIdentity = bytes.Repeat([]byte{'h'}, 4096)
	source.Namespace = []byte("transfer-base")
	source.Default = &storageEnrollmentDefaultSource{Workflow: 4, Branch: storageCreationPersonal, Settings: base.Project, Selected: base.Project, SelectedPath: []byte("/" + strings.Repeat("p", 16383)), Next: bytes.Repeat([]byte{'n'}, 255), ChainCount: 1, ChainDigest: storagebudget.Digest{41}, SourceNative: base.Project}
	raw, err := storageEnrollmentPeakEncodeSource(source)
	if err != nil || len(raw) != 25464 || storageEnrollmentPeakSourceMaximum != 25464 {
		t.Fatal("closed transfer source maximum", len(raw), err)
	}
	source.Default.SelectedPath = append(source.Default.SelectedPath, 'x')
	if _, err = storageEnrollmentPeakEncodeSource(source); err == nil {
		t.Fatal("oversize transfer source accepted")
	}
	source.Default.SelectedPath = []byte("/" + strings.Repeat("p", 4095))
	source.Default.Workflow = 2
	source.Default.ChainCount = 0
	source.Default.ChainDigest = storagebudget.Digest{}
	source.Default.SourceNative = storageOwnerPersistentObject{}
	source.Namespace = []byte("history-base")
	raw, err = storageEnrollmentPeakEncodeSource(source)
	if err != nil || len(raw) != 13106 || storageEnrollmentDefaultSourceMaximum != 13106 || storageEnrollmentSuccessorFamily(10, source) != 2 {
		t.Fatal("legacy source bytes/family changed", len(raw), err)
	}
	source.Default.SelectedPath = append(source.Default.SelectedPath, 'x')
	if _, err = storageEnrollmentPeakEncodeSource(source); err == nil {
		t.Fatal("old source inherited transfer path limit")
	}
}

func TestStorageTransferProjectionClosedWireRetainsBeforeChecksum(t *testing.T) {
	old, record, claim := ownerAdvisoryFixture(false)
	old.Workspaces[0].Definition.Generation = 7
	old.Workspaces[1].Definition.Generation = 9
	old.Roots[3].References = []storagebudget.ID{old.Workspaces[1].Definition.ID}
	ownerCatalogSeal(&old, &record, &claim)
	original := ownerAdvisoryWire(old)
	dictionary := ownerCatalogClone(old)
	dictionary.Roots[3].References = []storagebudget.ID{old.Workspaces[0].Definition.ID, old.Workspaces[1].Definition.ID}
	// The unchanged V1 bytes come from the existing handwritten field writer.
	legacy := epProjection(old, old, original)
	p, err := storageEnrollmentPeakReadProjection(legacy, old, original, record, claim)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := storageEnrollmentPeakWriteProjection(p, old, original, record, claim)
	if err != nil || !bytes.Equal(encoded, legacy) {
		t.Fatal("old V1 projection bytes changed", err)
	}
	// Independently authored v3: exact old membership is bit1 of root3's
	// canonical current edge interval; every other projected edge is retained.
	var refs [8192]byte
	edge := 0
	for _, row := range dictionary.Roots {
		for _, id := range row.References {
			keep := true
			if row.Definition.ID == old.Roots[3].Definition.ID {
				keep = id == old.Workspaces[1].Definition.ID
			}
			if keep {
				refs[edge/8] |= 1 << uint(edge%8)
			}
			edge++
		}
	}
	extended, base := epTransferProjectionBytes(old, dictionary, original, nil, refs, [512]byte{})
	p, err = storageEnrollmentPeakReadProjection(extended, dictionary, original, record, claim)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Catalog.Roots[3].References) != 1 || p.Catalog.Roots[3].References[0] != old.Workspaces[1].Definition.ID || p.Catalog.Workspaces[0].Definition.Generation != 7 || p.Catalog.Workspaces[1].Definition.Generation != 9 {
		t.Fatal("original membership/generations lost")
	}
	encoded, err = storageEnrollmentPeakWriteProjection(p, dictionary, original, record, claim)
	if err != nil || !bytes.Equal(encoded, extended) {
		t.Fatal("v3 extension is not canonical", err)
	}
	for _, mutation := range []string{"wrong-member", "reserved", "padding", "saved-checksum"} {
		bad := append([]byte{}, extended...)
		switch mutation {
		case "wrong-member":
			bad[base+16] ^= 1
		case "reserved":
			bad[base+10] = 1
		case "padding":
			bad[base+16+8191] = 128
		case "saved-checksum":
			bad[base-1] ^= 1
		}
		if _, err := storageEnrollmentPeakReadProjection(bad, dictionary, original, record, claim); err == nil {
			t.Fatal("mutated before image accepted", mutation)
		}
	}

	if !bytes.Equal(dictionary.Roots[3].References[0][:], old.Workspaces[0].Definition.ID[:]) || len(dictionary.Roots[3].References) != 2 {
		t.Fatal("projection changed after union")
	}
}

// Detached chain vectors never issue native qualification or mint custody.
// The full native verifier separately consumes the actual held chain in32
// parent windows; these vectors pin its immutable complete comparison image.
func TestStorageTransferChainCommitmentMaximumAndCorruption(t *testing.T) {
	catalog, _, base, _, _ := epCatalogs(t)
	if len(catalog.Objects) == 0 || len(catalog.Roots) == 0 {
		t.Fatal("fixture must retain nonempty baseline catalog")
	}
	names := make([]string, 8191)
	natives := make([]storageOwnerPersistentObject, len(names))
	for i := range names {
		names[i] = "a"
		natives[i] = epNative(uint64(100+i), 1)
	}
	source := base
	source.Namespace = []byte("transfer-root")
	source.Default = &storageEnrollmentDefaultSource{Workflow: 6, Branch: storageCreationPersonal, Settings: base.Project, Selected: natives[len(natives)-1], SelectedPath: []byte("/" + strings.Join(names, "/")), OwnSelf: 1, ChainCount: 8191, SourceNative: base.Project}
	if len(source.Default.SelectedPath) != 16382 {
		t.Fatal("maximum component lexical board")
	}
	digest, err := storageTransferChainDigest(source, epNative(1, 1), names, natives)
	if err != nil {
		t.Fatal(err)
	}
	source.Default.ChainDigest = digest
	raw, err := storageEnrollmentPeakEncodeSource(source)
	if err != nil {
		t.Fatal("8191 chain with unchanged nonempty catalog", err)
	}
	if len(catalog.Objects)+len(catalog.OwnerControls) > 8192 {
		t.Fatal("unexpected catalog growth")
	}
	decoded, err := storageEnrollmentPeakDecodeSource(raw)
	if err != nil || decoded.Default.ChainCount != 8191 || decoded.Default.ChainDigest != digest {
		t.Fatal("chain source lost", err)
	}
	// Altering an intermediate identity changes the complete commitment even
	// when the anchor/endpoint, names and path still match.
	changed := append([]storageOwnerPersistentObject{}, natives...)
	changed[4095].Incarnation++
	other, err := storageTransferChainDigest(source, epNative(1, 1), names, changed)
	if err != nil || other == digest {
		t.Fatal("intermediate native omitted", err)
	}
	badNames := append([]string{}, names...)
	badNames[4095] = "b"
	if _, err = storageTransferChainDigest(source, epNative(1, 1), badNames, natives); err == nil {
		t.Fatal("intermediate raw name not bound to lexical chain")
	}
	if _, err = storageTransferChainDigest(source, epNative(2, 1), names, natives); err != nil {
		t.Fatal(err)
	} else {
		other, _ = storageTransferChainDigest(source, epNative(2, 1), names, natives)
		if other == digest {
			t.Fatal("anchor omitted")
		}
	}
	for _, cut := range []int{1, 31, 32, 33} {
		if _, err = storageEnrollmentPeakDecodeSource(raw[:len(raw)-cut]); err == nil {
			t.Fatal("truncated chain commitment accepted", cut)
		}
	}
	appended := append(append([]byte{}, raw...), 0)
	if _, err = storageEnrollmentPeakDecodeSource(appended); err == nil {
		t.Fatal("unknown source suffix accepted")
	}
	// No discovered sibling edge inherits a captured chain claim. Membership
	// requires the exact native parent/name/child triple, not a common prefix.
	parent := storageDirectoryIdentity{Device: 1, Inode: 2}
	child := storageDirectoryIdentity{Device: 1, Inode: 3}
	walk := storageCensusWalk{transferEdges: map[storageTransferCensusEdge]bool{{parent: parent, child: child, name: "a"}: true}}
	if !walk.transferEdges[storageTransferCensusEdge{parent: parent, child: child, name: "a"}] || walk.transferEdges[storageTransferCensusEdge{parent: parent, child: child, name: "unexpected-deep-sibling"}] {
		t.Fatal("unexpected branch inherited captured edge")
	}
}

// Handwritten v3 wire companion: no implementation encoder records these
// bytes. Test fixtures here keep dictionary prefixes in their original order.
func epTransferProjectionBytes(c, dict storageOwnerCatalogV2, outer []byte, locations []storageEnrollmentTransferPriorLocation, refs [8192]byte, inactive [512]byte) ([]byte, int) {
	w := epBytes{}
	w.fixed(uint16(3))
	w.fixed(byte(0))
	w.fixed(byte(0))
	w.fixed(uint32(0))
	w.fixed(c.Version)
	w.fixed(c.Installation)
	w.fixed(c.QBinding)
	w.fixed(c.Sequence)
	w.data([]byte(c.Phase))
	w.fixed(c.Transition)
	w.fixed(c.PreviousCatalogDigest)
	w.fixed(c.BeforeDefinitionsDigest)
	w.fixed(c.AfterDefinitionsDigest)
	w.fixed(c.ExpectedQRevision)
	w.fixed(c.Policy)
	w.fixed(uint16(0))
	w.fixed(c.OriginalOwner.RegistryControl)
	for _, n := range []int{len(c.Objects), len(c.OwnerControls), len(c.Roots), len(c.Workspaces), len(c.Filesystems), len(c.Constraints), len(c.Profiles), len(c.OriginalOwner.Entries)} {
		w.fixed(uint16(n))
		for i := 0; i < n; i++ {
			w.fixed(uint16(i))
		}
	}
	for kind, ids := range [][]storagebudget.ID{c.BeforeDefinitionIDs.Roots, c.BeforeDefinitionIDs.Workspaces, c.BeforeDefinitionIDs.Filesystems, c.BeforeDefinitionIDs.Constraints} {
		w.fixed(uint16(len(ids)))
		for _, id := range ids {
			index := uint16(65535)
			switch kind {
			case 0:
				for i, row := range dict.Roots {
					if row.Definition.ID == id {
						index = uint16(i)
					}
				}
			case 1:
				for i, row := range dict.Workspaces {
					if row.Definition.ID == id {
						index = uint16(i)
					}
				}
			case 2:
				for i, row := range dict.Filesystems {
					if row.Definition.ID == id {
						index = uint16(i)
					}
				}
			case 3:
				for i, row := range dict.Constraints {
					if row.Definition.ID == id {
						index = uint16(i)
					}
				}
			}
			w.fixed(index)
		}
	}
	w.fixed(uint16(0))
	w.data(outer[:64])
	base := w.Len()
	w.Write([]byte("DSKTC3\x00\x00"))
	w.fixed(uint16(len(locations)))
	w.fixed(uint16(0))
	w.fixed(uint32(0))
	for _, row := range locations {
		w.fixed(row.Control)
		w.fixed(row.Selected)
		w.fixed(row.Previous)
		w.fixed(uint16(0))
	}
	w.Write(refs[:])
	w.Write(inactive[:])
	raw := append([]byte{}, w.Bytes()...)
	epSet32(raw[4:], uint32(len(raw)))
	return raw, base
}

func storageTransferNamespaceVector(t *testing.T, id byte, next string, inactive bool, prior storagebudget.ID) storageOwnerRoot {
	t.Helper()
	_, _, source, _, _ := epCatalogs(t)
	source.Namespace = []byte("transfer-base")
	source.Default = &storageEnrollmentDefaultSource{Workflow: 4, Branch: storageCreationPersonal, Settings: source.Project, SourceNative: source.Project, Selected: source.Project, SelectedPath: []byte("/"), Next: []byte(next), ChainDigest: storagebudget.Digest{41}}
	if prior != (storagebudget.ID{}) {
		source.Default.CreationPage = storageTransferNamespaceID(prior)
	}
	raw, err := storageEnrollmentPeakEncodeSource(source)
	if err != nil {
		t.Fatal(err)
	}
	flag := byte(0)
	if inactive {
		flag = 4
	}
	return storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{id}}, Role: 8, DirectoryObject: 0, Source: storageOwnerRootSource{Kind: 9, Relative: raw, ControlSelection: flag}}
}

func TestStorageTransferNamespaceChainsAreExclusiveAndAppendOnly(t *testing.T) {
	a := storageTransferNamespaceVector(t, 1, "a", true, storagebudget.ID{})
	b := storageTransferNamespaceVector(t, 2, "a", true, a.Definition.ID)
	c := storageTransferNamespaceVector(t, 3, "a", false, b.Definition.ID)
	sibling := storageTransferNamespaceVector(t, 4, "b", false, storagebudget.ID{})
	source, _ := storageEnrollmentPeakDecodeSource(a.Source.Relative)
	catalog := storageOwnerCatalogV2{Objects: []storageOwnerObject{{Persistent: source.Default.Selected}}, Roots: []storageOwnerRoot{a, b, c, sibling}}
	links, err := storageTransferNamespaceRelations(catalog)
	if err != nil || links[b.Definition.ID] != a.Definition.ID || links[c.Definition.ID] != b.Definition.ID {
		t.Fatal("multi-generation retirement lineage lost", err)
	}
	terminal, err := storageTransferNamespaceTerminal(catalog, a.Definition.ID)
	if err != nil || terminal.Definition.ID != c.Definition.ID {
		t.Fatal("retired direct successor must resolve to active terminal", err)
	}
	// A retired chain without a successor remains structural comparison data.
	// Actual native absence/Q zero/no promises are separate mandatory issuer gates.
	orphan := catalog
	orphan.Roots = []storageOwnerRoot{a}
	if _, err = storageTransferNamespaceRelations(orphan); err != nil {
		t.Fatal("retired unclaimed terminal shape refused", err)
	}
	for _, cut := range []string{"duplicate-next", "fork", "revival", "unknown-prior", "obsolete-audit-digest"} {
		bad := ownerCatalogClone(catalog)
		switch cut {
		case "duplicate-next":
			bad.Roots = append(bad.Roots, storageTransferNamespaceVector(t, 5, "a", false, storagebudget.ID{}))
		case "fork":
			bad.Roots = append(bad.Roots, storageTransferNamespaceVector(t, 5, "a", true, a.Definition.ID))
		case "revival":
			bad.Roots[0].Source.ControlSelection = 0
		case "unknown-prior":
			bad.Roots[2] = storageTransferNamespaceVector(t, 3, "a", false, storagebudget.ID{99})
		case "obsolete-audit-digest":
			raw := append([]byte{}, bad.Roots[2].Source.Relative...)
			decoded, e := storageEnrollmentPeakDecodeSource(raw)
			if e != nil {
				t.Fatal(e)
			}
			decoded.Default.LocationPayload = storagebudget.Digest{99}
			if _, e = storageEnrollmentPeakEncodeSource(decoded); e == nil {
				t.Fatal("Workflow4 nonzero LocationPayload accepted")
			}
			continue
		}
		if _, err = storageTransferNamespaceRelations(bad); err == nil {
			t.Fatal("contradictory namespace relation accepted", cut)
		}
	}
}

func TestStorageTransferEmptyAnchorAndOriginalSourceBinding(t *testing.T) {
	row := storageTransferNamespaceVector(t, 1, "next", false, storagebudget.ID{})
	source, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := storageTransferChainDigest(source, source.Default.Selected, []string{}, []storageOwnerPersistentObject{})
	if err != nil || digest == (storagebudget.Digest{}) {
		t.Fatal("exact nonowning slash anchor refused", err)
	}
	source.Default.ChainDigest = digest
	altered := source
	d := *source.Default
	altered.Default = &d
	d.SourceNative.Object++
	other, err := storageTransferChainDigest(altered, d.Selected, []string{}, []storageOwnerPersistentObject{})
	if err != nil || other == digest {
		t.Fatal("same settings/cohort but different source was omitted", err)
	}
	for _, cut := range []string{"destination", "other-workflow", "owning", "missing-next", "wrong-anchor"} {
		bad := source
		d := *source.Default
		bad.Default = &d
		anchor := d.Selected
		switch cut {
		case "destination":
			d.Workflow = 5
			bad.Namespace = []byte("transfer-data")
		case "other-workflow":
			d.Workflow = 2
			bad.Namespace = []byte("history-base")
		case "owning":
			d.OwnSelf = 1
		case "missing-next":
			d.Next = []byte{}
		case "wrong-anchor":
			anchor.Object++
		}
		if _, err = storageTransferChainDigest(bad, anchor, []string{}, []storageOwnerPersistentObject{}); err == nil {
			t.Fatal("generic empty-chain authority accepted", cut)
		}
	}
}

func TestStorageTransferProjectionMultipleControlsAndReferenceCohorts(t *testing.T) {
	n := storageTransferVector().Source
	dict := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}, Objects: []storageOwnerObject{{Persistent: n, Parent: storageOwnerNoObject}}}
	a, b, c := storagebudget.ID{11}, storagebudget.ID{12}, storagebudget.ID{13}
	for _, id := range []storagebudget.ID{a, b, c} {
		dict.Workspaces = append(dict.Workspaces, storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: id, Generation: 7}})
	}
	for control := 0; control < 2; control++ {
		dict.OwnerControls = append(dict.OwnerControls, storageOwnerControl{Role: 2, ParentObject: 0, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte{byte('a' + control)}, Location: storageOwnerLocation{Version: 1, Path: []byte("/new"), Previous: []byte("/old")}, MaxBytes: 16384})
		for j, path := range []string{"/old", "/older", "/new"} {
			native := n
			native.Object = uint64(2 + control*3 + j)
			dict.Objects = append(dict.Objects, storageOwnerObject{Persistent: native, Parent: storageOwnerNoObject})
			dict.Roots = append(dict.Roots, storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{byte(20 + control*3 + j)}}, Role: 4, DirectoryObject: uint32(len(dict.Objects) - 1), DomainObject: storageOwnerNoObject, WorkObject: storageOwnerNoObject, References: []storagebudget.ID{a, b, c}, Source: storageOwnerRootSource{Kind: 4, Path: []byte(path), ControlKind: 2, ControlIndex: uint32(control), ControlSelection: byte([]int{2, 3, 1}[j])}})
		}
		dict.OwnerControls[control].Location.Selected = dict.Objects[3+control*3].Persistent
		dict.OwnerControls[control].Location.PreviousSelected = dict.Objects[1+control*3].Persistent
	}
	p := storageEnrollmentPeakProjection{Catalog: storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}}, TransferPrior: &storageEnrollmentTransferPriorControl{Controls: []storageEnrollmentTransferPriorLocation{{Control: 0, Selected: 0, Previous: 1}, {Control: 1, Selected: 3, Previous: 4}}}}
	for k, n := range []int{7, 2, 6, 3, 0, 0, 0, 0} {
		for i := 0; i < n; i++ {
			p.Select[k] = append(p.Select[k], uint16(i))
		}
	}
	// Separate original cohorts for both controls: old roots retain A; newer
	// roots retain B+C. No global union is substituted for an original root.
	for i := 0; i < 6; i++ {
		for member := 0; member < 3; member++ {
			if i%3 == 0 && member == 0 || i%3 != 0 && member != 0 {
				edge := i*3 + member
				p.TransferPrior.References[edge/8] |= 1 << uint(edge%8)
			}
		}
	}
	before, err := storageEnrollmentPeakProjectionExpand(p, dict)
	if err != nil {
		t.Fatal(err)
	}
	if string(before.OwnerControls[0].Location.Path) != "/old" || string(before.OwnerControls[1].Location.Previous) != "/older" || len(before.Roots[0].References) != 1 || before.Roots[0].References[0] != a || len(before.Roots[4].References) != 2 || before.Roots[4].References[0] != b {
		t.Fatal("accumulated control/cohort reconstruction changed")
	}
	for _, cut := range []string{"duplicate-control", "foreign-control", "foreign-workspace", "reference-padding", "inactive-padding"} {
		bad := p
		prior := *p.TransferPrior
		prior.Controls = append([]storageEnrollmentTransferPriorLocation{}, prior.Controls...)
		bad.TransferPrior = &prior
		switch cut {
		case "duplicate-control":
			prior.Controls[1] = prior.Controls[0]
		case "foreign-control":
			prior.Controls[1].Control = 65535
		case "foreign-workspace":
			bad.Select[3] = []uint16{1, 2}
		case "reference-padding":
			prior.References[8191] = 128
		case "inactive-padding":
			prior.Inactive[511] = 128
		}
		if _, err = storageEnrollmentPeakProjectionExpand(bad, dict); err == nil {
			t.Fatal("noncanonical historical reconstruction accepted", cut)
		}
	}
}

func TestStorageTransferProjectionMaximumIndicesAndReferenceEdges(t *testing.T) {
	id := func(v int) (out storagebudget.ID) { out[0] = byte(v); out[1] = byte(v >> 8); out[2] = 1; return }
	native := storageTransferVector().Source
	dict := storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}, Objects: []storageOwnerObject{{Persistent: native, Parent: storageOwnerNoObject}}}
	p := storageEnrollmentPeakProjection{Catalog: storageOwnerCatalogV2{OriginalOwner: storageOwnerSource{RegistryControl: storageOwnerNoControl}}, TransferPrior: &storageEnrollmentTransferPriorControl{}}
	p.Select[0] = []uint16{0}
	for i := 0; i < 1024; i++ {
		dict.Workspaces = append(dict.Workspaces, storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: id(i), Generation: 7}})
		p.Select[3] = append(p.Select[3], uint16(i))
	}
	for i := 0; i < 4096; i++ {
		row := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: id(1024 + i)}, DirectoryObject: 0, DomainObject: storageOwnerNoObject, WorkObject: storageOwnerNoObject, Source: storageOwnerRootSource{ControlIndex: storageOwnerNoControl}}
		for j := 0; j < 16; j++ {
			row.References = append(row.References, id((i%64)*16+j))
		}
		dict.Roots = append(dict.Roots, row)
		p.Select[2] = append(p.Select[2], uint16(i))
	}
	for i := range p.TransferPrior.References {
		p.TransferPrior.References[i] = 255
	}
	old, err := storageEnrollmentPeakProjectionExpand(p, dict)
	if err != nil || len(old.Roots) != 4096 || len(old.Roots[4095].References) != 16 || old.Roots[4095].References[15] != id(1023) {
		t.Fatal("maximum canonical current-edge bit lost", err)
	}
	index, ok := storageTransferProjectionDefinitionIndex(dict, 0, id(5119))
	if !ok || index != 4095 {
		t.Fatal("maximum root index lost", index, ok)
	}
	index, ok = storageTransferProjectionDefinitionIndex(dict, 1, id(1023))
	if !ok || index != 1023 {
		t.Fatal("maximum workspace index lost", index, ok)
	}
	if _, ok = storageTransferProjectionDefinition(dict, 1, 4095); ok {
		t.Fatal("foreign-kind definition index accepted")
	}
	if _, ok = storageTransferProjectionDefinitionIndex(dict, 0, id(1023)); ok {
		t.Fatal("workspace ID substituted for root definition")
	}
	// A retained old membership removed from current dictionaries cannot be
	// reconstructed; no digest-only or spare-row fallback exists.
	before := ownerCatalogClone(dict)
	dict.Roots[4095].References = dict.Roots[4095].References[:15]
	if _, err = storageTransferHistoricalProjection(before, dict, make([]byte, storageOwnerCatalogBytes)); err == nil {
		t.Fatal("removed root/workspace membership silently omitted")
	}
}

func TestStorageTransferOwningCompletionCannotHideDefinitionChanges(t *testing.T) {
	before, _, _ := ownerCatalogFixture()
	after := ownerCatalogClone(before)
	if !storageTransferNoDefinitionChange(before, after) {
		t.Fatal("unchanged owning completion refused")
	}
	for _, cut := range []string{"source", "references", "policy", "owner", "registration-root"} {
		bad := ownerCatalogClone(after)
		switch cut {
		case "source":
			bad.Roots[0].Source.Path = append(bad.Roots[0].Source.Path, 'x')
		case "references":
			bad.Roots[0].References = append(bad.Roots[0].References, storagebudget.ID{99})
		case "policy":
			bad.Policy.ObservationMaxAgeNanos++
		case "owner":
			bad.OriginalOwner.RegistryControl++
		case "registration-root":
			bad.Roots = append(bad.Roots, bad.Roots[0])
		}
		if storageTransferNoDefinitionChange(before, bad) {
			t.Fatal("completion changed definition material", cut)
		}
	}
}

func TestStorageTransferObserverDistinguishesOwningRootAndNamespaceViews(t *testing.T) {
	first := storageTransferNamespaceVector(t, 1, "a", false, storagebudget.ID{})
	second := storageTransferNamespaceVector(t, 2, "b", false, storagebudget.ID{})
	source, err := storageEnrollmentPeakDecodeSource(first.Source.Relative)
	if err != nil {
		t.Fatal(err)
	}
	owner := &storageBudgetOwner{}
	owning := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{3}}, DirectoryObject: 0}
	a := &storageAdvisoryObserver{owner: owner, catalog: storageOwnerCatalogV2{Objects: []storageOwnerObject{{Persistent: source.Default.Selected}}, Roots: []storageOwnerRoot{owning, first, second}}, roots: map[storagebudget.ID]storageOwnerRoot{owning.Definition.ID: owning, first.Definition.ID: first, second.Definition.ID: second}}
	selected, err := a.originalOwningNative(source.Default.Selected)
	if err != nil || selected.Definition.ID != owning.Definition.ID {
		t.Fatal("disjoint views obscured ordinary owner", err)
	}
	bound := &storageRootBinding{owner: owner, row: storageOwnerRootValue(first.Definition), source: &storageOriginalSourceRoot{role: 8, persistent: source.Default.Selected, scope: &source}, restrictedNamespace: "a"}
	bound.self = bound
	if row, provisional, err := a.exactCatalogBinding(bound); err != nil || provisional || row.Definition.ID != first.Definition.ID {
		t.Fatal("explicit namespace identity refused", err)
	}
	// A namespace-only anchor is observable through its exact catalog binding;
	// reverse native lookup never grants ordinary ownership of that anchor.
	a.catalog.Roots = []storageOwnerRoot{first, second}
	if _, _, err = a.exactCatalogBinding(bound); err != nil {
		t.Fatal("namespace-only explicit ID refused", err)
	}
	if _, err = a.originalOwningNative(source.Default.Selected); err == nil {
		t.Fatal("namespace anchor became owning root")
	}
	for _, cut := range []string{"wrong-id", "wrong-generation", "wrong-native", "wrong-next", "retired-as-active", "overlap"} {
		copy := *bound
		copy.self = &copy
		src := *bound.source
		copy.source = &src
		catalog := ownerCatalogClone(a.catalog)
		saved := a.catalog
		a.catalog = catalog
		switch cut {
		case "wrong-id":
			copy.row.ID = storagebudget.ID{99}
		case "wrong-generation":
			copy.row.Generation++
		case "wrong-native":
			src.persistent.Object++
		case "wrong-next":
			copy.restrictedNamespace = "b"
		case "retired-as-active":
			copy.transferInactive = true
		case "overlap":
			a.catalog.Roots = append(a.catalog.Roots, storageTransferNamespaceVector(t, 4, "a", false, storagebudget.ID{}))
		}
		_, _, err = a.exactCatalogBinding(&copy)
		a.catalog = saved
		if err == nil {
			t.Fatal("foreign bound namespace observation accepted", cut)
		}
	}
	a.catalog.Roots = []storageOwnerRoot{owning, owning, first}
	if _, err = a.originalOwningNative(source.Default.Selected); err == nil {
		t.Fatal("duplicate ordinary owner accepted")
	}
	provisional := &storageRootBinding{owner: owner, row: storagebudget.Root{ID: owning.Definition.ID}}
	if _, ok, err := a.exactCatalogBinding(provisional); err != nil || !ok {
		t.Fatal("ordinary constructor's provisional identity changed", err)
	}
	provisional.row.ID = first.Definition.ID
	if _, _, err = a.exactCatalogBinding(provisional); err == nil {
		t.Fatal("namespace admitted through provisional ordinary constructor")
	}
}

// Independent default-source mapping material over these handwritten inputs.
func storageTransferDefaultMappingVector(t *testing.T, source storageEnrollmentPeakSource, c storageOwnerCatalogV2, row storageOwnerRoot) storagebudget.Digest {
	t.Helper()
	p := row.Membership
	if len(p.ExistingObjects) != 0 || len(c.Constraints) != 1 || len(c.Profiles) != 1 {
		t.Fatal("mapping vector has unexpected original material")
	}
	original := storageEnrollmentPeakSourceMappingMaterial{Installation: source.Installation, QBinding: source.QBinding, Project: source.Project, Workspace: c.Workspaces[0].Definition, OwnerKey: source.OwnerKey, HistoryIdentity: source.HistoryIdentity, HomeSource: source.HomeSource, Namespace: source.Namespace, Generation: source.Generation, Filesystem: c.Filesystems[0], CapacityCount: source.CapacityCount, CapacityRefs: source.CapacityRefs, Constraints: c.Constraints, Membership: storageOwnerMembershipMaterial{Backend: p.Backend, ProfileID: p.ProfileID, ProfileVersion: p.ProfileVersion, ProfileEvidence: p.ProfileEvidence, Pool: p.Pool, VolumeUUID: p.VolumeUUID, ContainerUUID: p.ContainerUUID, User: p.User, Group: p.Group, Project: p.Project, Creation: p.Creation, ExistingObjects: []storageOwnerObjectMaterial{}}, Profiles: c.Profiles}
	return epHash("desk-default-history-source-mapping-v1", epJSON(struct {
		Original storageEnrollmentPeakSourceMappingMaterial
		Default  storageEnrollmentDefaultSource
	}{original, *source.Default}))
}

// Complete logical catalog inputs with disjoint owning and nonowning cohorts.
// Nonzero chain commitments are comparison data, never native absence, a
// retirement witness, or authorization to run an effect.
func storageTransferCatalogViewVector(t *testing.T, c storageOwnerCatalogV2, anchor storageOwnerRoot, id byte, refs []storagebudget.ID, active bool, next string, original storageOwnerPersistentObject) storageOwnerRoot {
	t.Helper()
	row := anchor
	row.Definition.ID = storagebudget.ID{id}
	row.Definition.Locator = "retained-transfer-view"
	row.Definition.Workspace = storagebudget.ID{}
	row.Role, row.Category = 8, storagebudget.Control
	row.References = append([]storagebudget.ID{}, refs...)
	row.DomainObject, row.WorkObject = storageOwnerNoObject, storageOwnerNoObject
	row.Source = storageOwnerRootSource{Kind: 9, OwnerKey: append([]byte{}, c.Workspaces[0].OwnerKey...), Path: append([]byte{}, anchor.Source.Path...), ControlIndex: storageOwnerNoControl, ControlSelection: 4}
	if active {
		row.Source.ControlSelection = 0
	}
	source := storageEnrollmentPeakSource{Installation: c.Installation, QBinding: c.QBinding, Project: epNative(100, 1), Workspace: c.Workspaces[0].Definition.ID, WorkspaceGeneration: c.Workspaces[0].Definition.Generation, OwnerKey: append([]byte{}, c.Workspaces[0].OwnerKey...), HistoryIdentity: append([]byte{}, c.Workspaces[0].HistoryIdentity...), Filesystem: row.Definition.Filesystem, FilesystemIdentity: c.Filesystems[0].Definition.Identity, FilesystemGeneration: c.Filesystems[0].Definition.Generation, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Namespace: []byte("transfer-base"), Generation: row.Definition.Generation, Default: &storageEnrollmentDefaultSource{Workflow: 4, Branch: storageCreationPersonal, Settings: c.Objects[c.Roots[0].DirectoryObject].Persistent, Selected: c.Objects[anchor.DirectoryObject].Persistent, SelectedPath: append([]byte{}, anchor.Source.Path...), Next: []byte(next), ChainCount: uint16(len(strings.Split(strings.TrimPrefix(string(anchor.Source.Path), "/"), "/"))), ChainDigest: storagebudget.Digest{41}, SourceNative: original}}
	source.HomeSource = epHomeSource(source, c)
	source.CapacityBinding = storageTransferDefaultMappingVector(t, source, c, row)
	wire := epBytes{}
	wire.Write(storageDefaultSourceVector(source))
	wire.fixed(source.Default.ChainCount)
	wire.fixed(source.Default.ChainDigest)
	wire.fixed(source.Default.SourceNative)
	row.Source.Relative = append([]byte{}, wire.Bytes()...)
	row.Definition.Identity = epHash("desk-chat-transfer-ancestor-scope-v1", row.Source.Relative)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range c.Constraints {
		constraints = append(constraints, v.Definition)
	}
	d := row.Definition
	var err error
	row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, c.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func storageTransferRetainedViewsFixture(t *testing.T, targetRefs []storagebudget.ID) (storageOwnerCatalogV2, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	_, c, _, record, claim := epCatalogs(t)
	c.Roots[3].References = []storagebudget.ID{c.Workspaces[0].Definition.ID}
	c.Roots[3].Source.OwnerKey = []byte(ownerTestHome)
	c.OwnerControls[0].OwnerKey = []byte(ownerTestHome)
	c.OwnerControls[0].RelationDigest, _ = c.controlDigest(c.OwnerControls[0])
	c.Roots[3].Definition.Identity = ownerCatalogIdentity(c, c.Roots[3])
	sourceDefinition := c.Roots[3].Definition
	c.Roots[3].Definition.CapacityBinding, _ = storagebudget.CapacityBindingFor(storagebudget.Root{ID: sourceDefinition.ID, Owner: sourceDefinition.Owner, Identity: sourceDefinition.Identity, Filesystem: sourceDefinition.Filesystem, Generation: sourceDefinition.Generation, CapacityCount: sourceDefinition.CapacityCount, CapacityRefs: sourceDefinition.CapacityRefs}, c.Filesystems[0].Definition, []storagebudget.CapacityConstraint{c.Constraints[0].Definition})
	target := c.Roots[3]
	target.Definition.ID = storagebudget.ID{9}
	target.Definition.Locator = "existing-empty-target"
	target.Definition.Workspace = storagebudget.ID{}
	target.References = append([]storagebudget.ID{}, targetRefs...)
	target.Source.Path = []byte("/target")
	target.Source.ControlSelection = 3
	target.DirectoryObject = uint32(len(c.Objects))
	c.Objects = append(c.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: epNative(150, 1), OwnerClass: 2, ModeClass: 1}, storageOwnerObject{Role: 23, Source: 3, Parent: target.DirectoryObject, Name: []byte(dataMarkerName), Persistent: epNative(151, 2), OwnerClass: 2, ModeClass: 2, FixedLength: 99})
	target.Definition.Identity = ownerCatalogIdentity(c, target)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range c.Constraints {
		constraints = append(constraints, v.Definition)
	}
	d := target.Definition
	var err error
	target.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, c.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	c.Roots = append(c.Roots, target)
	c.BeforeDefinitionIDs.Roots = append(c.BeforeDefinitionIDs.Roots, target.Definition.ID)
	refs := []storagebudget.ID{c.Workspaces[0].Definition.ID, c.Workspaces[1].Definition.ID}
	original := c.Objects[c.Roots[3].DirectoryObject].Persistent
	c.Roots = append(c.Roots, storageTransferCatalogViewVector(t, c, c.Roots[3], 91, refs, false, "absent-source-child", original), storageTransferCatalogViewVector(t, c, target, 92, refs, false, "absent-target-child", original))
	epSealDefinitions(&c)
	if err = storageEnrollmentPeakValidateCatalog(c, record, claim); err != nil {
		t.Fatal("complete retained-view input catalog", err)
	}
	return c, record, claim
}

func TestStorageTransferRetainedViewsPreserveOwningSelectionAndReferences(t *testing.T) {
	for _, b := range [][]storagebudget.ID{{}, {{2}}} {
		before, _, _ := storageTransferRetainedViewsFixture(t, b)
		after := ownerCatalogClone(before)
		after.Roots[3].Source.ControlSelection = 2
		after.Roots[8].Source.ControlSelection = 1
		want := []storagebudget.ID{{1}}
		if len(b) != 0 {
			want = []storagebudget.ID{{1}, {2}}
		}
		after.Roots[8].References = want
		control := &after.OwnerControls[0]
		control.Location.Path, control.Location.Previous = []byte("/target"), []byte("/data/a")
		control.Location.Selected, control.Location.PreviousSelected = epNative(150, 1), before.Objects[before.Roots[3].DirectoryObject].Persistent
		var err error
		control.RelationDigest, err = after.controlDigest(*control)
		if err != nil {
			t.Fatal(err)
		}
		row := after.Roots[8]
		source := storageEnrollmentPeakSource{Installation: after.Installation, QBinding: after.QBinding, Project: epNative(100, 1), Workspace: after.Workspaces[0].Definition.ID, WorkspaceGeneration: after.Workspaces[0].Definition.Generation, OwnerKey: append([]byte{}, after.Workspaces[0].OwnerKey...), HistoryIdentity: append([]byte{}, after.Workspaces[0].HistoryIdentity...), Filesystem: row.Definition.Filesystem, FilesystemIdentity: after.Filesystems[0].Definition.Identity, FilesystemGeneration: 1, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Namespace: []byte("transfer-data"), Generation: 1, Default: &storageEnrollmentDefaultSource{Workflow: 5, Branch: storageCreationPersonal, Settings: before.Objects[0].Persistent, Selected: epNative(150, 1), SelectedPath: []byte("/target"), Marker: epNative(151, 2), Location: epNative(152, 2), LocationPayload: storagebudget.Digest{51}, CreationPage: storagebudget.Digest{52}, ChainCount: 1, ChainDigest: storagebudget.Digest{53}, SourceNative: control.Location.PreviousSelected}}
		source.HomeSource = epHomeSource(source, after)
		source.CapacityBinding = storageTransferDefaultMappingVector(t, source, after, row)
		r := storageTransferRecord{TargetEmpty: true, TargetBefore: epNative(150, 1), Source: source.Default.SourceNative}
		if !storageTransferRootReferencesRetained(r, before, before.Roots[8], after.Roots[8]) || !storageTransferCatalogRetains(r, before, after) {
			t.Fatal("exact B union A refused beside independent views", b)
		}
		if !storageTransferSourceSelection(before, after, source) {
			t.Fatal("one owning target plus nonowning target view refused", b)
		}
		for _, cut := range []string{"extra-view-cohort", "altered-original-source", "duplicate-source-owner", "duplicate-target-owner", "foreign-source-bytes"} {
			badBefore, badAfter := ownerCatalogClone(before), ownerCatalogClone(after)
			switch cut {
			case "extra-view-cohort":
				badAfter.Roots[8].References = append(badAfter.Roots[8].References, storagebudget.ID{99})
			case "altered-original-source":
				badAfter.Roots[3].References = []storagebudget.ID{{2}}
			case "duplicate-source-owner":
				duplicate := badBefore.Roots[3]
				duplicate.Definition.ID = storagebudget.ID{93}
				badBefore.Roots = append(badBefore.Roots, duplicate)
				badAfter.Roots = append(badAfter.Roots, duplicate)
			case "duplicate-target-owner":
				duplicate := badBefore.Roots[8]
				duplicate.Definition.ID = storagebudget.ID{93}
				badBefore.Roots = append(badBefore.Roots, duplicate)
				badAfter.Roots = append(badAfter.Roots, duplicate)
			case "foreign-source-bytes":
				badAfter.Roots[9].Source.Relative[0] ^= 1
			}
			if storageTransferSourceSelection(badBefore, badAfter, source) {
				t.Fatal("changed owning selection relation accepted", cut, b)
			}
		}
	}
}

func TestStorageTransferRefreshSelectsFrozenNamespaceAtSharedAnchor(t *testing.T) {
	catalog, record, claim := storageTransferRetainedViewsFixture(t, []storagebudget.ID{})
	original := catalog.Objects[catalog.Roots[3].DirectoryObject].Persistent
	view := storageTransferCatalogViewVector(t, catalog, catalog.Roots[0], 93, []storagebudget.ID{{1}}, true, "refresh-target", original)
	catalog.Roots = append(catalog.Roots, view)
	epSealDefinitions(&catalog)
	if err := storageEnrollmentPeakValidateCatalog(catalog, record, claim); err != nil {
		t.Fatal("ordinary config plus exact active destination view", err)
	}
	bank := storageTransferVector()
	bank.Mode = storageTransferMove
	bank.Installation, bank.Owner, bank.QBinding = catalog.Installation, catalog.QBinding, catalog.QBinding
	bank.Settings, bank.Parent, bank.Basis = catalog.Objects[0].Persistent, catalog.Objects[0].Persistent, catalog.Objects[0].Persistent
	bank.Source, bank.Requester = original, epNative(100, 1)
	bank.RequesterWorkspace, bank.RequesterGeneration = storagebudget.ID{1}, 1
	bank.ArchiveBytes, bank.ArchiveDigest, bank.CreatedAt = 0, storagebudget.Digest{}, ""
	creation := storageCreationFixture(t, storageCreationHistory)
	bank.Tail = creation.Tail
	bank.Tail.Installation, bank.Tail.QBinding, bank.Tail.Project, bank.Tail.Bridge = bank.Installation, bank.QBinding, bank.Requester, bank.Operation
	bank.OldAbsent, bank.TargetEmpty = true, true
	bank.MarkerBytes, bank.MarkerDigest = 1, storagebudget.Digest{61}
	bank.NewPointer = []byte(`{"version":1,"path":"/config/refresh-target","previous":"/data/a"}`)
	bank.ExistingPrefix = uint16(len("/config/"))
	bank.Descendants = make([]storageTransferDescendant, 1)
	var err error
	bank.Groups, err = storageTransferBuildGroups(catalog, original, []storageTransferCharge{{root: catalog.Roots[0].Definition.ID, category: storagebudget.Control, references: storageTransferNoReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}, {root: view.Definition.ID, category: storagebudget.Control, references: storageTransferSourceReferences, physical: storagebudget.Vector{Directories: 1}, attribution: storagebudget.Vector{Directories: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	bank.Changes = make([]storagebudget.Change, len(bank.Groups))
	bank.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := storageEnrollmentPeakDecodeSource(view.Source.Relative)
	if err != nil {
		t.Fatal(err)
	}
	binding := &storageRootBinding{row: storageOwnerRootValue(view.Definition), source: &storageOriginalSourceRoot{role: 8, persistent: bank.Parent, scope: &scope}, restrictedNamespace: "refresh-target", references: []storagebudget.ID{{1}}}
	ordinary := &storageRootBinding{row: storageOwnerRootValue(catalog.Roots[0].Definition)}
	op := &chatTransferOperation{record: bank, catalog: catalog, preparation: &chatTransferPreparation{epoch: &storageEnrollmentEpoch{originalRoots: []*storageRootBinding{ordinary, binding}}}, path: &storageTransferPathWitness{path: "/config/refresh-target", names: []string{"config", "refresh-target"}, native: bank.Parent, missing: 1}}
	op.self = op
	got, err := op.originalTransferDescendantBinding()
	if err != nil || got != binding {
		t.Fatal("ordinary Control group obscured exact frozen destination", err)
	}
	for _, cut := range []string{"different-next", "different-view-id", "inactive-view", "stale-membership", "changed-native", "changed-binding-refs", "changed-source", "duplicate-owning-parent", "consumed-refresh"} {
		bad := copiedValue(op)
		bad.self = &bad
		bad.catalog = ownerCatalogClone(catalog)
		bad.record.Groups = append([]storageTransferGroup{}, bank.Groups...)
		copyBinding := *binding
		copySource := *binding.source
		copyScope := scope
		d := *scope.Default
		copyScope.Default = &d
		copySource.scope, copyBinding.source = &copyScope, &copySource
		bad.preparation = &chatTransferPreparation{epoch: &storageEnrollmentEpoch{originalRoots: []*storageRootBinding{ordinary, &copyBinding}}}
		switch cut {
		case "different-next":
			copyBinding.restrictedNamespace, d.Next = "other-target", []byte("other-target")
		case "different-view-id":
			copyBinding.row.ID = storagebudget.ID{94}
		case "inactive-view":
			copyBinding.transferInactive = true
		case "stale-membership":
			bad.record.Groups[len(bank.Groups)-1].Membership[0] ^= 1
		case "changed-native":
			copySource.persistent.Object++
		case "changed-binding-refs":
			copyBinding.references = []storagebudget.ID{{2}}
		case "changed-source":
			d.SourceNative.Object++
		case "duplicate-owning-parent":
			duplicate := bad.catalog.Roots[0]
			duplicate.Definition.ID = storagebudget.ID{94}
			bad.catalog.Roots = append(bad.catalog.Roots, duplicate)
			bad.record.Catalog, _ = storageTransferCatalogDigest(bad.catalog)
			// Rebind the two exact original group memberships independently so
			// this reaches the duplicate-owning-parent check, not a stale hash.
			for i, group := range bad.record.Groups {
				material := epBytes{}
				material.fixed(bad.record.Catalog)
				material.fixed(group.Component.Root)
				material.fixed(group.Component.Category)
				material.fixed(group.Component.RootGeneration)
				if group.Component.Root == view.Definition.ID {
					material.fixed(storagebudget.ID{1})
					material.fixed(uint64(1))
				}
				bad.record.Groups[i].Membership = epHash("desk-storage-whole-transfer-reference-set-v1", material.Bytes())
			}
		case "consumed-refresh":
			bad.board = []storageTransferOrdinal{{kind: storageTransferMarkerOrdinal}}
			bad.capacityChecks = []bool{true}
			if err := bad.refreshOriginalTransferCapacity(t.Context(), storageTransferMarkerOrdinal, 0); err == nil {
				t.Fatal("consumed ordinal refreshed again")
			}
			continue
		}
		if _, err := bad.originalTransferDescendantBinding(); err == nil {
			t.Fatal("foreign destination refresh binding accepted", cut)
		}
	}
}
