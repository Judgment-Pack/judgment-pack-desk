package desk

import (
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageWholeTransferExactReferenceAlgebra(t *testing.T) {
	a, b, c := storagebudget.ID{1}, storagebudget.ID{2}, storagebudget.ID{3}
	for _, row := range []struct {
		name         string
		root, source []storagebudget.ID
		want         [8][]storagebudget.ID
	}{
		{"overlap", []storagebudget.ID{b, c}, []storagebudget.ID{a, b}, [8][]storagebudget.ID{{}, {b, c}, {a, b}, {a, b, c}, {b}, {c}, {a}, {a, c}}},
		{"identical", []storagebudget.ID{a, b}, []storagebudget.ID{a, b}, [8][]storagebudget.ID{{}, {a, b}, {a, b}, {a, b}, {a, b}, {}, {}, {}}},
		{"disjoint", []storagebudget.ID{c}, []storagebudget.ID{a, b}, [8][]storagebudget.ID{{}, {c}, {a, b}, {a, b, c}, {}, {c}, {a, b}, {a, b, c}}},
		{"empty-root", []storagebudget.ID{}, []storagebudget.ID{a}, [8][]storagebudget.ID{{}, {}, {a}, {a}, {}, {}, {a}, {a}}},
	} {
		t.Run(row.name, func(t *testing.T) {
			for selector, want := range row.want {
				got, err := storageTransferSelectedReferences(row.root, row.source, byte(selector))
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatal(selector, got, want, err)
				}
			}
		})
	}
}

func storageTransferAdmissionCatalog(references int) storageOwnerCatalogV2 {
	vector := storageTransferVector()
	component := vector.Groups[0].Component
	catalog := storageOwnerCatalogV2{Installation: vector.Installation, QBinding: vector.QBinding, Objects: []storageOwnerObject{{Persistent: vector.Source}, {Persistent: vector.Source}}, Filesystems: []storageOwnerFilesystem{{Definition: storagebudget.Filesystem{ID: component.Filesystem, Identity: vector.Groups[0].FilesystemIdentity, Generation: 1}}}, Constraints: []storageOwnerConstraint{{Definition: storagebudget.CapacityConstraint{ID: component.Capacity[0].Constraint, Generation: 1, Resources: 3}}}}
	catalog.Objects[1].Persistent.Object = 2
	for index := 0; index < references; index++ {
		id := storagebudget.ID{byte(index + 1), byte((index + 1) >> 8)}
		catalog.Workspaces = append(catalog.Workspaces, storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: id, Generation: 7}})
	}
	for index := 0; index < 2; index++ {
		root := storageOwnerRoot{DirectoryObject: uint32(index), Definition: storagebudget.RegisteredRoot{ID: storagebudget.ID{byte(20 + index)}, Owner: component.Owner, Identity: storagebudget.Digest{byte(30 + index)}, Filesystem: component.Filesystem, Generation: 1, Locator: "original", CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: component.Capacity[0].Constraint, Generation: 1}}, CapacityBinding: component.CapacityBinding}, References: []storagebudget.ID{}}
		catalog.Roots = append(catalog.Roots, root)
	}
	return catalog
}

func TestStorageWholeTransferHistoricalEmptyTargetAttribution(t *testing.T) {
	catalog := storageTransferAdmissionCatalog(3)
	a, b, c := catalog.Workspaces[0].Definition.ID, catalog.Workspaces[1].Definition.ID, catalog.Workspaces[2].Definition.ID
	catalog.Roots[0].References = []storagebudget.ID{a, b}
	catalog.Roots[1].References = []storagebudget.ID{b, c}
	target := catalog.Roots[1].Definition.ID
	// N is newly copied material. E is the positively measured existing marker
	// and root: physically already charged, newly attributed only to source\old.
	n := storagebudget.Vector{Bytes: 10, Files: 1}
	e := storagebudget.Vector{Bytes: 4, Files: 1, Directories: 1}
	charges := []storageTransferCharge{{root: target, category: storagebudget.History, references: storageTransferUnionReferences, physical: n, attribution: n, capacity: storagebudget.CapacityVector{Bytes: 65536, Inodes: 1}}, {root: target, category: storagebudget.History, references: storageTransferSourceOnlyReferences, attribution: e}}
	groups, err := storageTransferBuildGroups(catalog, catalog.Objects[0].Persistent, charges)
	if err != nil || len(groups) != 2 {
		t.Fatal(groups, err)
	}
	record := storageTransferVector()
	record.RequesterWorkspace = a
	record.RequesterGeneration = 7
	record.Groups = groups
	record.Changes = make([]storagebudget.Change, len(groups))
	record.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	fanout, err := storageTransferReferences(record, catalog)
	if err != nil {
		t.Fatal(err)
	}
	physical := storagebudget.Vector{}
	capacity := storagebudget.CapacityVector{}
	actual := map[storagebudget.ID]storagebudget.Vector{}
	for index, group := range groups {
		physical, err = storageVectorAdd(physical, group.Component.Physical)
		if err != nil {
			t.Fatal(err)
		}
		capacity.Bytes += group.Component.Capacity[0].Peak.Bytes
		capacity.Inodes += group.Component.Capacity[0].Peak.Inodes
		for member, reference := range fanout.Workspaces {
			if fanout.Memberships[index][member/8]&(1<<uint(member%8)) != 0 {
				if actual[reference.ID] != (storagebudget.Vector{}) {
					t.Fatal("duplicate expanded key")
				}
				actual[reference.ID] = group.Component.Attribution
			}
		}
	}
	if physical != n || capacity != (storagebudget.CapacityVector{Bytes: 65536, Inodes: 1}) || actual[a] != (storagebudget.Vector{Bytes: 14, Files: 2, Directories: 1}) || actual[b] != n || actual[c] != n {
		t.Fatal("physical/attribution duplicated or lost", physical, capacity, actual)
	}
	// The original catalog and caller charge rows remain detached.
	groups[0].Component.Attribution.Bytes = 999
	fanout.Memberships[0][0] = 0
	if catalog.Roots[1].References[0] != b || charges[0].attribution != n {
		t.Fatal("caller slice aliased")
	}
	// Changed generation or an invented member cannot match the original plan.
	record.Groups, err = storageTransferBuildGroups(catalog, catalog.Objects[0].Persistent, charges)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Workspaces[0].Definition.Generation++
	if _, err = storageTransferReferences(record, catalog); err == nil {
		t.Fatal("generation mismatch accepted")
	}
}

func TestStorageWholeTransferFull1024MemberPartition(t *testing.T) {
	catalog := storageTransferAdmissionCatalog(1024)
	for _, workspace := range catalog.Workspaces {
		catalog.Roots[0].References = append(catalog.Roots[0].References, workspace.Definition.ID)
		catalog.Roots[1].References = append(catalog.Roots[1].References, workspace.Definition.ID)
	}
	// Canonical ID order is byte order, independent of the handwritten numeric
	// generator. The same closed set is used by both roots, without a subset.
	catalog.Roots[0].References = storageUniqueIDs(catalog.Roots[0].References)
	catalog.Roots[1].References = storageUniqueIDs(catalog.Roots[1].References)
	groups, err := storageTransferBuildGroups(catalog, catalog.Objects[0].Persistent, []storageTransferCharge{{root: catalog.Roots[1].Definition.ID, category: storagebudget.History, references: storageTransferUnionReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil || len(groups) != 1 {
		t.Fatal(len(groups), err)
	}
	record := storageTransferVector()
	record.RequesterWorkspace = catalog.Workspaces[0].Definition.ID
	record.RequesterGeneration = 7
	record.Groups = groups
	record.Changes = make([]storagebudget.Change, 1)
	record.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	fanout, err := storageTransferReferences(record, catalog)
	if err != nil || len(fanout.Workspaces) != 1024 {
		t.Fatal(err)
	}
	for index, bits := range fanout.Memberships[0] {
		if bits != 255 {
			t.Fatal("missing original members", index, bits)
		}
	}
	// All generations and all membership bits are detached from the catalog.
	fanout.Workspaces[1023].Generation = 99
	fanout.Memberships[0][127] = 0
	fresh, err := storageTransferReferences(record, catalog)
	if err != nil || fresh.Workspaces[1023].Generation != 7 || fresh.Memberships[0][127] != 255 {
		t.Fatal("dictionary or membership alias", err)
	}
}

func TestStorageWholeTransferEqualRootAndSourceControlQuantities(t *testing.T) {
	catalog := storageTransferAdmissionCatalog(3)
	a, b, c := catalog.Workspaces[0].Definition.ID, catalog.Workspaces[1].Definition.ID, catalog.Workspaces[2].Definition.ID
	catalog.Roots[0].References = []storagebudget.ID{a, b}
	catalog.Roots[1].References = []storagebudget.ID{b, c}
	v := storagebudget.Vector{Bytes: 1, Files: 1}
	root := catalog.Roots[1].Definition.ID
	groups, err := storageTransferBuildGroups(catalog, catalog.Objects[0].Persistent, []storageTransferCharge{{root: root, category: storagebudget.Control, references: storageTransferRootReferences, physical: v, attribution: v, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}, {root: root, category: storagebudget.Control, references: storageTransferSourceReferences, physical: v, attribution: v, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil || len(groups) != 2 {
		t.Fatal("symmetric difference unrepresentable", groups, err)
	}
	amounts := map[storagebudget.ID]storagebudget.Vector{}
	physical := storagebudget.Vector{}
	capacity := storagebudget.CapacityVector{}
	symmetric := false
	for _, group := range groups {
		ids, err := storageTransferSelectedReferences(catalog.Roots[1].References, catalog.Roots[0].References, group.Flags)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if amounts[id] != (storagebudget.Vector{}) {
				t.Fatal("expanded key duplicated")
			}
			amounts[id] = group.Component.Attribution
		}
		physical, err = storageVectorAdd(physical, group.Component.Physical)
		if err != nil {
			t.Fatal(err)
		}
		capacity.Bytes += group.Component.Capacity[0].Peak.Bytes
		capacity.Inodes += group.Component.Capacity[0].Peak.Inodes
		symmetric = symmetric || group.Flags == storageTransferSymmetricDifferenceReferences
	}
	if !symmetric || amounts[a] != v || amounts[c] != v || amounts[b] != (storagebudget.Vector{Bytes: 2, Files: 2}) || physical != (storagebudget.Vector{Bytes: 2, Files: 2}) || capacity != (storagebudget.CapacityVector{Bytes: 2, Inodes: 2}) {
		t.Fatal("wrong independent control attribution", amounts, physical, capacity)
	}
	record := storageTransferVector()
	record.RequesterWorkspace = a
	record.RequesterGeneration = 7
	record.Groups = groups
	record.Changes = make([]storagebudget.Change, len(groups))
	record.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := storageTransferEncode(record)
	if err != nil {
		t.Fatal(err)
	}
	record, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := storageTransferProjectIntent(record, catalog, 0, 1, storagebudget.Digest{}, storageIntentPrepared)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storageEncodeIntent(projected, catalog.Installation, catalog.QBinding)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := storageDecodeIntent(page, 0, catalog.Installation, catalog.QBinding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storageTransferCatalogIntentFanout(decoded, catalog); err != nil {
		t.Fatal("symmetric selector lost by compact witness", err)
	}
	invalid := decoded
	copy := *decoded.Transfer
	invalid.Transfer = &copy
	invalid.Transfer.Selectors[0] = storageTransferSymmetricDifferenceReferences + 1
	if _, err := storageEncodeIntent(invalid, catalog.Installation, catalog.QBinding); err == nil {
		t.Fatal("unassigned selector encoded")
	}

	// This constructor vector does not establish that a supported route's final
	// effect/control board reaches the overlap. That complete board has its own
	// separate <=16 physical-group proof and actual route fixtures.
	for _, sets := range []struct{ root, source, want []storagebudget.ID }{{[]storagebudget.ID{b, c}, []storagebudget.ID{a, b}, []storagebudget.ID{a, c}}, {[]storagebudget.ID{a, b}, []storagebudget.ID{a, b}, []storagebudget.ID{}}, {[]storagebudget.ID{a}, []storagebudget.ID{c}, []storagebudget.ID{a, c}}, {[]storagebudget.ID{}, []storagebudget.ID{a}, []storagebudget.ID{a}}} {
		got, err := storageTransferSelectedReferences(sets.root, sets.source, storageTransferSymmetricDifferenceReferences)
		if err != nil || !reflect.DeepEqual(got, sets.want) {
			t.Fatal("closed symmetric difference", got, err)
		}
	}
	if _, err := storageTransferSelectedReferences(nil, nil, storageTransferSymmetricDifferenceReferences+1); err == nil {
		t.Fatal("unassigned selector accepted")
	}
}

func TestStorageWholeTransferChargeProducerCompleteGroupBound(t *testing.T) {
	for _, geometry := range []string{"existing-empty-target", "new-target-separate-parent", "new-target-settings-parent-alias", "new-target-config-parent-alias", "export"} {
		t.Run(geometry, func(t *testing.T) {
			catalog := storageTransferAdmissionCatalog(3)
			a, b, c := catalog.Workspaces[0].Definition.ID, catalog.Workspaces[1].Definition.ID, catalog.Workspaces[2].Definition.ID
			catalog.Roots[0].Role = 4
			catalog.Roots[1].Role = 4
			catalog.Roots[0].References = []storagebudget.ID{a, b}
			catalog.Roots[1].References = []storagebudget.ID{b, c}
			for index, role := range []byte{1, 2, 3, 8} {
				root := catalog.Roots[1]
				root.Role = role
				root.Definition.ID = storagebudget.ID{byte(22 + index)}
				root.Definition.Identity = storagebudget.Digest{byte(32 + index)}
				root.DirectoryObject = uint32(len(catalog.Objects))
				root.References = []storagebudget.ID{}
				if role == 3 || role == 8 {
					root.References = []storagebudget.ID{b, c}
				}
				object := catalog.Objects[1]
				object.Persistent.Object = uint64(3 + index)
				catalog.Objects = append(catalog.Objects, object)
				catalog.Roots = append(catalog.Roots, root)
			}
			record := storageTransferVector()
			record.Mode = storageTransferRestore
			record.Source = catalog.Objects[0].Persistent
			record.Settings = catalog.Objects[4].Persistent
			record.Parent = catalog.Objects[5].Persistent
			record.TargetEmpty = true
			record.NewPointer = []byte(`{"version":1,"path":"/new-target","previous":"/original"}`)
			record.InputBytes = 100
			record.MarkerBytes = 17
			hash := strings.Repeat("a", 64)
			for _, name := range []string{"conversations-" + hash + ".json", "ai-memory-" + hash + ".json", "attachment-" + hash + "-00000000-0000-0000-0000-000000000000.json", "assistant-dialect-read-recovery-" + hash + "-arun_" + strings.Repeat("b", 32) + ".json"} {
				record.Outputs = append(record.Outputs, storageTransferOutput{Name: name, Bytes: 3, SHA256: storagebudget.Digest{7}})
			}
			record.Descendants = make([]storageTransferDescendant, 33)
			switch geometry {
			case "existing-empty-target":
				record.TargetBefore = catalog.Objects[1].Persistent
				record.Parent = record.TargetBefore
				record.Descendants = nil
				record.MarkerBytes = 0
				record.TargetExisting = storagebudget.Vector{Bytes: 17, Files: 1, Directories: 1}
			case "new-target-settings-parent-alias":
				record.Parent = record.Settings
			case "new-target-config-parent-alias":
				record.Parent = catalog.Objects[2].Persistent
			case "export":
				record.Mode = storageTransferExport
				record.ArchiveBytes = 100
				record.InputBytes = 0
				record.Outputs = nil
				record.Descendants = nil
				record.TargetEmpty = false
				record.NewPointer = nil
				record.MarkerBytes = 0
			}
			groups, err := storageTransferChargeBoard(record, catalog)
			if err != nil {
				t.Fatal(err)
			}
			maximum := 9
			if geometry == "export" {
				maximum = 3
			}
			if len(groups) > maximum {
				t.Fatal("complete producer exceeds bound", len(groups))
			}
			if geometry == "new-target-settings-parent-alias" && len(groups) != 9 {
				t.Fatal("worst reference-atom alias not exercised", len(groups))
			}
			// Independent expanded-key accounting catches both duplicate physical
			// assignment and attribution overlap after control/root coalescing.
			seen := map[struct {
				root, workspace storagebudget.ID
				category        byte
			}]bool{}
			physical := storagebudget.Vector{}
			for _, group := range groups {
				var root storageOwnerRoot
				for _, candidate := range catalog.Roots {
					if candidate.Definition.ID == group.Component.Root {
						root = candidate
					}
				}
				members, err := storageTransferSelectedReferences(root.References, catalog.Roots[0].References, group.Flags)
				if err != nil {
					t.Fatal(err)
				}
				for _, id := range members {
					key := struct {
						root, workspace storagebudget.ID
						category        byte
					}{root.Definition.ID, id, group.Component.Category}
					if seen[key] {
						t.Fatal("duplicate expanded key")
					}
					seen[key] = true
				}
				physical, err = storageVectorAdd(physical, group.Component.Physical)
				if err != nil {
					t.Fatal(err)
				}
			}
			expected := storagebudget.Vector{Bytes: 100, Files: 1}
			if geometry != "export" {
				expected.Bytes += uint64(len(record.NewPointer)) + 12 + record.MarkerBytes
				expected.Files += 5
				if record.MarkerBytes != 0 {
					expected.Files++
				}
				expected.Directories = uint64(len(record.Descendants))
			}
			if physical != expected {
				t.Fatal("physical charge lost or duplicated", physical, expected)
			}
		})
	}
}

func TestStorageWholeTransferQManifestBindsCompleteClosedPlan(t *testing.T) {
	catalog := storageTransferAdmissionCatalog(1024)
	for _, workspace := range catalog.Workspaces {
		catalog.Roots[0].References = append(catalog.Roots[0].References, workspace.Definition.ID)
	}
	catalog.Roots[0].References = storageUniqueIDs(catalog.Roots[0].References)
	record := storageTransferVector()
	record.RequesterWorkspace = catalog.Workspaces[0].Definition.ID
	record.RequesterGeneration = 7
	var err error
	record.Groups, err = storageTransferBuildGroups(catalog, record.Source, []storageTransferCharge{{root: catalog.Roots[0].Definition.ID, category: storagebudget.Control, references: storageTransferSourceReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	record.Changes = make([]storagebudget.Change, len(record.Groups))
	record.Catalog, err = storageTransferCatalogDigest(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := storageTransferEncode(record)
	if err != nil {
		t.Fatal(err)
	}
	record, err = storageTransferDecode(raw)
	if err != nil {
		t.Fatal(err)
	}
	request, err := storageTransferOriginalRequest(record, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Entries) != 0 || len(request.Operation) != 106 || !strings.HasPrefix(request.Operation, "transfer:01000000000000000000000000000000:") || !strings.HasSuffix(request.Operation, hex.EncodeToString(record.Plan[:])) {
		t.Fatal("Q operation does not bind the complete host plan", request.Operation)
	}
	ticket := storagebudget.Ticket{Token: storagebudget.Token{ID: storagebudget.ID{44}, Sequence: 1, Generation: 1}, State: storagebudget.Reserved, PolicyRevision: 1, Workflow: request.Workflow, Operation: request.Operation, Owner: request.Owner, Components: request.Components, Fanout: request.Fanout}
	manifest, err := storageEnrollmentPeakQEncodeFanoutManifest(storageEnrollmentPeakQTicketValue(ticket), storageEnrollmentPeakQHeldAnchor{Installation: storageEnrollmentPeakQID(record.Installation), Binding: storageEnrollmentPeakQDigest(record.QBinding)})
	if err != nil {
		t.Fatal(err)
	}
	// One fixed ticket plus the global dictionary and sixteen exact membership
	// rows. Complete names/digests/ordinals reside in the separately closed host
	// bank, whose full Plan is in the original Q Operation above. No object entry
	// lane or per-output admission is hidden in this bound.
	meaningful := 128 + 16384 + 16 + 1024*24 + 16*128
	if meaningful != 43152 || len(manifest) != 524288 || !storageAllZero(manifest[meaningful:]) {
		t.Fatal("joint manifest representation bound", meaningful, len(manifest))
	}
	zeroPlan := record
	zeroPlan.Plan = storagebudget.Digest{}
	if _, err := storageTransferOriginalRequest(zeroPlan, catalog); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("Q operation adopted a missing immutable host plan", err)
	}
	changed := record
	changed.CreatedAt = "2026-11-02T00:00:00Z"
	if _, err := storageTransferOriginalRequest(changed, catalog); err == nil {
		t.Fatal("same Q operation adopted a changed immutable host plan")
	}
}
