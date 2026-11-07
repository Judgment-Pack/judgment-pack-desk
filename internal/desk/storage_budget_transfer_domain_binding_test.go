package desk

import (
	"context"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Handwritten comparison material only: no original native custody, Q admission
// or qualification is issued by this fixture. The added target has a lower
// persistent key than config, so the production seal must move config's index.
func storageTransferDomainReindexFixture(t *testing.T) (*storageBudgetConstruction, storageOwnerCatalogV2, storageOwnerObject) {
	t.Helper()
	before, _, _ := storageTransferRetainedViewsFixture(t, []storagebudget.ID{})
	after := ownerCatalogClone(before)
	settings := before.Objects[0].Persistent
	original := before.Objects[before.Roots[3].DirectoryObject].Persistent
	target, marker := epNative(1, 1), epNative(2, 2)
	if storageOwnerPOCompare(target, settings) >= 0 {
		t.Fatal("fixture no longer forces config reindexing")
	}

	row := after.Roots[3]
	row.Definition.ID = storagebudget.ID{94}
	row.Definition.Locator = "owning-reindexed-target"
	row.Source.Path = []byte("/config/refresh-target")
	row.DirectoryObject = uint32(len(after.Objects))
	after.Objects = append(after.Objects,
		storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: target, OwnerClass: 2, ModeClass: 1},
		storageOwnerObject{Role: 23, Source: 3, Parent: row.DirectoryObject, Name: []byte(dataMarkerName), Persistent: marker, OwnerClass: 2, ModeClass: 2, FixedLength: 1})
	row.Definition.Identity = ownerCatalogIdentity(after, row)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range after.Constraints {
		constraints = append(constraints, v.Definition)
	}
	d := row.Definition
	var err error
	row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Generation: d.Generation, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs}, after.Filesystems[0].Definition, constraints)
	if err != nil {
		t.Fatal(err)
	}
	after.Roots = append(after.Roots, row)
	after.Roots[3].Source.ControlSelection = 2
	control := &after.OwnerControls[0]
	control.Location.Path, control.Location.Previous = []byte("/config/refresh-target"), []byte("/data/a")
	control.Location.Selected, control.Location.PreviousSelected = target, original
	control.RelationDigest, err = after.controlDigest(*control)
	if err != nil {
		t.Fatal(err)
	}
	if err = storageTransferSealCatalog(&after); err != nil {
		t.Fatal(err)
	}

	bank := storageTransferVector()
	bank.Mode, bank.Phase, bank.Generation = storageTransferMove, storageTransferRetired, 8
	bank.Installation, bank.Owner, bank.QBinding = before.Installation, before.QBinding, before.QBinding
	bank.Settings, bank.Parent, bank.Basis = settings, settings, settings
	bank.Source, bank.Requester = original, epNative(100, 1)
	bank.RequesterWorkspace, bank.RequesterGeneration = before.Workspaces[0].Definition.ID, before.Workspaces[0].Definition.Generation
	bank.ArchiveBytes, bank.ArchiveDigest, bank.CreatedAt = 0, storagebudget.Digest{}, ""
	bank.Tail = storageCreationFixture(t, storageCreationHistory).Tail
	bank.Tail.Installation, bank.Tail.QBinding, bank.Tail.Project, bank.Tail.Bridge = bank.Installation, bank.QBinding, bank.Requester, bank.Operation
	bank.InitialToken = storagebudget.Token{Slot: 0, ID: storagebudget.ID{9}, Sequence: 1, Generation: 1}
	bank.Token = bank.InitialToken
	bank.Token.Generation += 2
	bank.OldPointer = []byte(`{"version":1,"path":"/data/a"}`)
	bank.OldPointerNative = epNative(152, 2)
	bank.NewPointer = []byte(`{"version":1,"path":"/config/refresh-target","previous":"/data/a"}`)
	bank.ExistingPrefix = uint16(len("/config/"))
	bank.Descendants = []storageTransferDescendant{{State: 2, Object: target.Object, Incarnation: target.Incarnation}}
	bank.TargetEmpty = true
	bank.MarkerBytes, bank.MarkerDigest, bank.MarkerState, bank.MarkerNative = 1, storagebudget.Digest{61}, 2, marker
	bank.PointerState, bank.PointerNative = 2, epNative(3, 2)
	bank.Groups, err = storageTransferBuildGroups(before, original, []storageTransferCharge{{root: before.Roots[3].Definition.ID, category: storagebudget.History, references: storageTransferSourceReferences, physical: storagebudget.Vector{Bytes: 1, Files: 1}, attribution: storagebudget.Vector{Bytes: 1, Files: 1}, capacity: storagebudget.CapacityVector{Bytes: 1, Inodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	bank.Changes = make([]storagebudget.Change, len(bank.Groups))
	bank.Catalog, err = storageTransferCatalogDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	if err = storageTransferSelectionRelation(bank, before, after); err != nil {
		t.Fatal("complete owning selection relation", err)
	}

	c := &storageBudgetConstruction{owner: &storageBudgetOwner{}}
	c.self = c
	c.sources = &storageOriginalSources{construction: c, owner: before.OriginalOwner, objects: before.Objects, controls: before.OwnerControls, configObject: 0}
	epoch := &storageEnrollmentEpoch{c: c}
	epoch.self = epoch
	preparation := &chatTransferPreparation{epoch: epoch}
	preparation.self = preparation
	op := &chatTransferOperation{preparation: preparation, record: bank, catalog: before}
	op.self = op
	preparation.operation, epoch.transfer = op, preparation
	c.owner.admission.enrollment, c.owner.admission.phase = epoch, storageAdmissionEnrollment
	for _, object := range after.Objects {
		if object.Role == 18 && string(object.Name) == ".data.lock" {
			if object.Parent == c.sources.configObject || after.Objects[object.Parent].Persistent != settings {
				t.Fatal("seal failed to translate the exact config domain")
			}
			return c, after, object
		}
	}
	t.Fatal("missing original domain")
	return nil, storageOwnerCatalogV2{}, storageOwnerObject{}
}

func TestStorageTransferOwningDomainCatalogReindex(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{
		"reindexed-selection", "ordinary-same-index", "unregistered-reindex",
		"wrong-domain-role", "parent-out-of-bounds", "source-config-out-of-bounds",
		"wrong-config-persistent", "wrong-domain-parent", "changed-domain-material",
		"wrong-candidate-owner", "wrong-retained-owner", "foreign-sources",
		"foreign-construction", "unsealed-construction", "unsealed-epoch",
		"wrong-epoch-construction", "missing-operation", "stale-operation",
		"finished-preparation", "wrong-phase", "unpublished-pointer",
		"changed-token", "changed-selection",
	} {
		t.Run(name, func(t *testing.T) {
			c, catalog, domain := storageTransferDomainReindexFixture(t)
			sources := c.sources
			epoch := c.owner.admission.enrollment
			preparation, op := epoch.transfer, epoch.transfer.operation
			want := false
			switch name {
			case "reindexed-selection":
				want = true
			case "ordinary-same-index":
				catalog = op.catalog
				domain = catalog.Objects[catalog.Roots[0].DomainObject]
				c.owner.admission.enrollment = nil
				want = true
			case "unregistered-reindex":
				c.owner.admission.enrollment = nil
			case "wrong-domain-role":
				domain.Role = 19
			case "parent-out-of-bounds":
				domain.Parent = uint32(len(catalog.Objects))
			case "source-config-out-of-bounds":
				sources.configObject = uint32(len(sources.objects))
			case "wrong-config-persistent":
				sources.objects = append([]storageOwnerObject{}, sources.objects...)
				sources.objects[sources.configObject].Persistent.Object = 999
			case "wrong-domain-parent":
				domain.Parent = 0 // the newly sorted target, not original config
			case "changed-domain-material":
				for i := range catalog.Objects {
					if catalog.Objects[i].Role == 18 {
						catalog.Objects[i].Name = []byte("foreign.lock")
						domain = catalog.Objects[i]
					}
				}
			case "wrong-candidate-owner":
				catalog.OriginalOwner.HomeOwner = "foreign"
			case "wrong-retained-owner":
				sources.owner.HomeOwner = "foreign"
			case "foreign-sources":
				copy := *sources
				sources = &copy
			case "foreign-construction":
				sources.construction = &storageBudgetConstruction{}
			case "unsealed-construction":
				c.self = nil
			case "unsealed-epoch":
				epoch.self = nil
			case "wrong-epoch-construction":
				epoch.c = &storageBudgetConstruction{}
			case "missing-operation":
				preparation.operation = nil
			case "stale-operation":
				op.self = nil
			case "finished-preparation":
				preparation.finished = true
			case "wrong-phase":
				op.record.Phase = storageTransferSettled
			case "unpublished-pointer":
				op.record.PointerState = 0
			case "changed-token":
				op.record.Token.Generation++
			case "changed-selection":
				catalog.OwnerControls[0].Location.Selected = epNative(999, 1)
			}
			if got := c.ownerBindingDomainMatches(ctx, catalog, sources, domain); got != want {
				t.Fatalf("domain match=%v, want %v", got, want)
			}
		})
	}
}
