package desk

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageOwnerCatalogV2HomeAndCapacityTwins(t *testing.T) {
	for _, name := range []string{"home-owner", "home-object", "home-registry-sentinel", "home-duplicate-folder-casefold", "home-duplicate-entry", "home-managed-history", "home-managed-storage-owner", "home-managed-runner", "home-project-relative-history", "home-folder-portability", "home-workspace-relation", "legacy-home-fields", "orphan-data-control", "orphan-provider-identity", "marker-role3-with-control0"} {
		t.Run(name, func(t *testing.T) {
			c, r, cl := ownerCatalogFixture()
			ownerCatalogHome(&c)
			switch name {
			case "home-owner":
				c.OriginalOwner.HomeOwner = strings.Repeat("a", 64)
			case "home-object":
				c.OriginalOwner.HomeObject.Object = 99
			case "home-registry-sentinel":
				c.OriginalOwner.RegistryControl = storageOwnerNoControl
			case "home-duplicate-folder-casefold":
				c.OriginalOwner.Entries[1].Folder = []byte("PROJECT")
			case "home-duplicate-entry":
				c.OriginalOwner.Entries[1].DeskID = c.OriginalOwner.Entries[0].DeskID
			case "home-managed-history":
				c.OriginalOwner.Entries[1].HistoryIdentity = []byte("/other")
			case "home-managed-storage-owner":
				c.OriginalOwner.Entries[1].StorageOwner = strings.Repeat("e", 32)
			case "home-managed-runner":
				c.OriginalOwner.Entries[1].RunnerWorkspace = strings.Repeat("e", 32)
			case "home-project-relative-history":
				c.OriginalOwner.Entries[0].HistoryIdentity = []byte("relative")
			case "home-folder-portability":
				c.OriginalOwner.Entries[0].Folder = []byte("CON")
			case "home-workspace-relation":
				c.Workspaces[2].HistoryIdentity = []byte("/other")
			case "legacy-home-fields":
				c.OriginalOwner.Kind = 1
			case "orphan-data-control":
				c.Roots[3].Source.ControlKind = 0
				c.Roots[3].Source.ControlIndex = storageOwnerNoControl
				c.Roots[3].Source.ControlSelection = 0
			case "orphan-provider-identity":
				c.Objects[22].Parent = 18
			case "marker-role3-with-control0":
				c.Roots[2].Source.ControlKind = 0
				c.Roots[2].Source.ControlIndex = storageOwnerNoControl
			}
			ownerCatalogSeal(&c, &r, &cl)
			if err := c.validate(r, cl); err == nil {
				t.Fatal("invalid original/use metadata accepted with recomputed digests")
			}
		})
	}
	for _, name := range []string{"ext4-bytes-only-backing", "ext4-dynamic-inodes", "quota-subject-mismatch", "quota-subject-missing-ref", "apfs-container-volume-not-absent", "apfs-container-pool-not-self", "apfs-volume-pool-mismatch", "apfs-volume-container-mismatch", "apfs-missing-volume-ref", "apfs-extra-linux-subject", "apfs-finite-inodes", "apfs-inode-reference", "apfs-duplicate-container", "apfs-wrong-volume"} {
		t.Run(name, func(t *testing.T) {
			c, r, cl := ownerCatalogFixture()
			if strings.HasPrefix(name, "apfs-") {
				ownerCatalogAPFS(&c, &r)
			}
			switch name {
			case "ext4-bytes-only-backing":
				c.Constraints[0].Definition.Resources = 1
				c.Constraints[0].Definition.Headroom.Inodes = 0
			case "ext4-dynamic-inodes":
				c.Filesystems[0].Definition.InodeModel = 2
				c.Filesystems[0].Definition.InodeConstraint = storagebudget.ID{}
			case "quota-subject-mismatch":
				ownerCatalogQuota(&c)
				c.Roots[0].Membership.User.Subject = 1
			case "quota-subject-missing-ref":
				ownerCatalogQuota(&c)
				c.Roots[0].Definition.CapacityCount = 3
				c.Roots[0].Definition.CapacityRefs[3] = storagebudget.CapacityRef{}
			case "apfs-container-volume-not-absent":
				c.Constraints[0].VolumeUUID = [16]byte{1}
			case "apfs-container-pool-not-self":
				c.Constraints[0].Pool = storagebudget.ID{11}
			case "apfs-volume-pool-mismatch":
				c.Constraints[1].Pool = storagebudget.ID{11}
			case "apfs-volume-container-mismatch":
				c.Constraints[1].ContainerUUID = [16]byte{3}
			case "apfs-missing-volume-ref":
				c.Roots[0].Definition.CapacityCount = 1
				c.Roots[0].Definition.CapacityRefs[1] = storagebudget.CapacityRef{}
			case "apfs-extra-linux-subject":
				c.Roots[0].Membership.User = storageOwnerSubject{State: 1, Kind: 1}
			case "apfs-finite-inodes":
				c.Filesystems[0].Definition.InodeModel = 1
			case "apfs-inode-reference":
				c.Filesystems[0].Definition.InodeConstraint = storagebudget.ID{10}
			case "apfs-duplicate-container":
				v := c.Constraints[0]
				v.Definition.ID = storagebudget.ID{12}
				v.Definition.Identity = storagebudget.Digest{12}
				v.Pool = v.Definition.ID
				c.Constraints = append(c.Constraints, v)
			case "apfs-wrong-volume":
				c.Constraints[1].VolumeUUID = [16]byte{3}
			}
			ownerCatalogSeal(&c, &r, &cl)
			if err := c.validate(r, cl); err == nil {
				t.Fatal("invalid capacity tuple accepted with recomputed digests")
			}
		})
	}
	t.Run("nonutf8-home-history-preserved", func(t *testing.T) {
		c, r, cl := ownerCatalogFixture()
		ownerCatalogHome(&c)
		path := []byte{'/', 'p', 0xff}
		c.OriginalOwner.Entries[0].HistoryIdentity = path
		c.Workspaces[2].HistoryIdentity = path
		ownerCatalogSeal(&c, &r, &cl)
		raw, err := storageOwnerEncodeCatalogV2(c, r, cl)
		if err != nil {
			t.Fatal(err)
		}
		got, err := storageOwnerDecodeCatalogV2(raw, r, cl)
		if err != nil || string(got.OriginalOwner.Entries[0].HistoryIdentity) != string(path) {
			t.Fatal("lossy home identity")
		}
	})
}
