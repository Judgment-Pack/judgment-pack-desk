package desk

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func ownerCatalogHome(c *storageOwnerCatalogV2) {
	c.OriginalOwner.Kind = 2
	c.OriginalOwner.HomePath = []byte("/home")
	c.OriginalOwner.HomeObject = storageOwnerPersistentObject{1, [16]byte{1}, 40, 0, 1, 1}
	c.OriginalOwner.RegistryControl = 1
	c.OriginalOwner.Entries = []storageOwnerHomeRelation{{strings.Repeat("c", 32), []byte("project"), 1, []byte("/project"), strings.Repeat("a", 64), strings.Repeat("f", 64)}, {strings.Repeat("f", 32), []byte("managed"), 2, []byte("desk:" + strings.Repeat("f", 32)), strings.Repeat("f", 32), strings.Repeat("f", 32)}}
	c.Objects = append(c.Objects, storageOwnerObject{Role: 3, Source: 2, Parent: math.MaxUint32, Name: []byte{}, Persistent: c.OriginalOwner.HomeObject, OwnerClass: 1, ModeClass: 1})
	c.OwnerControls = append(c.OwnerControls, storageOwnerControl{Role: 1, ParentObject: 0, Name: []byte("desks.json"), OwnerKind: 1, OwnerKey: []byte(ownerTestHome), Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}, MaxBytes: 1048576})
	c.Workspaces = append(c.Workspaces, storageOwnerWorkspace{storagebudget.RegisteredWorkspace{ID: storagebudget.ID{3}, Generation: 1, Limits: c.Policy.Workspace}, 2, []byte(strings.Repeat("c", 32)), []byte("/project")})
	c.Roots[2].References = append(c.Roots[2].References, storagebudget.ID{3})
}
func ownerCatalogAPFS(c *storageOwnerCatalogV2, r *storageOwnerRecordV2) {
	for i := range c.Objects {
		p := &c.Objects[i].Persistent
		p.Backend = 2
		p.Incarnation = 1
		p.IncarnationKind = 2
	}
	for _, p := range []*storageOwnerPersistentObject{&r.Config, &r.Anchor, &r.BootstrapLock, &r.UseLock, &r.HomeLease, &r.CatalogA, &r.CatalogB} {
		p.Backend = 2
		p.Incarnation = 1
		p.IncarnationKind = 2
	}
	c.OwnerControls[0].Location.Selected = c.Objects[20].Persistent
	c.Profiles[0].Backend = 2
	c.Filesystems[0].Backend = 2
	c.Filesystems[0].ContainerUUID = [16]byte{2}
	c.Filesystems[0].Definition.InodeModel = 2
	c.Filesystems[0].Definition.InodeConstraint = storagebudget.ID{}
	c.Constraints = []storageOwnerConstraint{{Definition: storagebudget.CapacityConstraint{ID: storagebudget.ID{10}, Identity: storagebudget.Digest{10}, Generation: 1, Resources: 1, Headroom: storagebudget.CapacityVector{Bytes: 100}}, Kind: 5, Pool: storagebudget.ID{10}, ContainerUUID: [16]byte{2}}, {Definition: storagebudget.CapacityConstraint{ID: storagebudget.ID{11}, Identity: storagebudget.Digest{11}, Generation: 1, Resources: 1, Headroom: storagebudget.CapacityVector{Bytes: 100}}, Kind: 6, Pool: storagebudget.ID{10}, VolumeUUID: [16]byte{1}, ContainerUUID: [16]byte{2}}}
	for i := range c.Roots {
		v := &c.Roots[i]
		v.Membership.Backend = 2
		v.Membership.Pool = storagebudget.ID{10}
		v.Membership.ContainerUUID = [16]byte{2}
		v.Membership.User = storageOwnerSubject{}
		v.Membership.Group = storageOwnerSubject{}
		v.Membership.Project = storageOwnerSubject{}
		v.Definition.CapacityCount = 2
		v.Definition.CapacityRefs[1] = storagebudget.CapacityRef{Constraint: storagebudget.ID{11}, Generation: 1}
	}
}
func ownerCatalogQuota(c *storageOwnerCatalogV2) {
	for k := uint8(1); k <= 3; k++ {
		s := storageOwnerSubject{State: 3, Kind: k, Subject: 0, NamespaceMap: storagebudget.Digest{2}, Constraint: storagebudget.ID{10 + k}}
		c.Constraints = append(c.Constraints, storageOwnerConstraint{storagebudget.CapacityConstraint{ID: s.Constraint, Identity: storagebudget.Digest{10 + k}, Generation: 1, Resources: 3, Headroom: storagebudget.CapacityVector{Bytes: 10, Inodes: 1}}, k + 1, storagebudget.ID{1}, [16]byte{1}, [16]byte{}, s})
		for i := range c.Roots {
			r := &c.Roots[i]
			switch k {
			case 1:
				r.Membership.User = s
			case 2:
				r.Membership.Group = s
			case 3:
				r.Membership.Project = s
			}
			r.Definition.CapacityRefs[k] = storagebudget.CapacityRef{Constraint: s.Constraint, Generation: 1}
			r.Definition.CapacityCount = 4
		}
	}
}

func TestStorageOwnerCatalogV2PositiveBranches(t *testing.T) {
	for _, name := range []string{"desk-home-enrolled-project-and-managed", "legacy-managed-workspace", "all-three-enforced-subjects-including-zero", "apfs-shared-container-volume", "source1-private-parent-history", "source3-private-parent-history", "source1-private-parent-role20", "source3-private-parent-role20", "private-boundary-history-role20", "history-boundary-role21", "source1-config-history-role21", "external-relative-maximum", "absolute-unix-backslash-and-nonutf8", "zero-mapping-evidence-is-detached-only", "root-parent-forward-in-id-order"} {
		t.Run(name, func(t *testing.T) {
			c, r, cl := ownerCatalogFixture()
			switch name {
			case "desk-home-enrolled-project-and-managed":
				ownerCatalogHome(&c)
			case "legacy-managed-workspace":
				c.Workspaces[1].OwnerKind = 2
				c.Workspaces[1].OwnerKey = []byte(strings.Repeat("f", 32))
				c.Workspaces[1].HistoryIdentity = []byte("desk:" + strings.Repeat("f", 32))
			case "all-three-enforced-subjects-including-zero":
				ownerCatalogQuota(&c)
			case "apfs-shared-container-volume":
				ownerCatalogAPFS(&c, &r)
			case "source1-private-parent-history": // Baseline role21 under role20/Source1.
			case "source3-private-parent-history":
				c.Objects[24].Parent = 18
				c.Objects[24].Source = 3
				c.Objects[24].OwnerClass = 2
				c.Roots[6].Source.ParentRoot = storagebudget.ID{3}
				c.Roots[6].Source.Path = []byte("/desk/private/history")
			case "source1-private-parent-role20":
				c.Objects[24].Role = 20
			case "source3-private-parent-role20":
				c.Objects[24].Role = 20
				c.Objects[24].Parent = 18
				c.Objects[24].Source = 3
				c.Objects[24].OwnerClass = 2
				c.Roots[6].Source.ParentRoot = storagebudget.ID{3}
				c.Roots[6].Source.Path = []byte("/desk/private/history")
			case "private-boundary-history-role20":
				c.Objects[20].Role = 20
			case "history-boundary-role21": // Baseline selected Source3 boundary.
			case "source1-config-history-role21":
				c.Objects[24].Parent = 0
				c.Roots[6].Source.ParentRoot = storagebudget.ID{1}
				c.Roots[6].Source.Path = []byte("/config/history")
			case "external-relative-maximum":
				c.Roots[2].Source.Relative = []byte(strings.Repeat(strings.Repeat("x", 1024)+"/", 63) + strings.Repeat("x", 1024))
			case "absolute-unix-backslash-and-nonutf8":
				c.Roots[2].Source.Path = []byte{'/', 'd', '\\', 0xff}
			case "zero-mapping-evidence-is-detached-only":
				c.Profiles[0].PersistentEvidence = storagebudget.Digest{}
				c.Profiles[0].MappingEvidence = storagebudget.Digest{}
				c.Profiles[0].QuotaEvidence = storagebudget.Digest{}
				c.Profiles[0].CounterEvidence = storagebudget.Digest{}
				c.Profiles[0].AllowanceEvidence = storagebudget.Digest{}
				for i := range c.Roots {
					c.Roots[i].Membership.User.NamespaceMap = storagebudget.Digest{}
					c.Roots[i].Membership.Group.NamespaceMap = storagebudget.Digest{}
					c.Roots[i].Membership.Project.NamespaceMap = storagebudget.Digest{}
				}
			case "root-parent-forward-in-id-order":
				parent, child := c.Roots[5], c.Roots[6]
				parent.Definition.ID = storagebudget.ID{8}
				child.Source.ParentRoot = parent.Definition.ID
				c.Roots[5], c.Roots[6] = child, parent
			}
			ownerCatalogSeal(&c, &r, &cl)
			raw, err := storageOwnerEncodeCatalogV2(c, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = storageOwnerDecodeCatalogV2(raw, r, cl); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStorageOwnerCatalogV2EnvelopeAndClosedJSON(t *testing.T) {
	c, r, cl := ownerCatalogFixture()
	payload := ownerCatalogJSON(c)
	for _, name := range []string{"short", "long", "prototype-magic", "version", "header-size", "zero-sequence", "sequence-disagreement", "reserved", "hash", "padding", "oversize-length"} {
		t.Run(name, func(t *testing.T) {
			raw := ownerCatalogWire(payload, 1)
			switch name {
			case "short":
				raw = raw[:len(raw)-1]
			case "long":
				raw = append(raw, 0)
			case "prototype-magic":
				raw[6] = '1'
			case "version":
				raw[8] = 3
			case "header-size":
				raw[12] = 63
			case "zero-sequence":
				clear(raw[16:24])
			case "sequence-disagreement":
				raw[16] = 2
				ownerTestChecksum(raw)
			case "reserved":
				raw[28] = 1
				ownerTestChecksum(raw)
			case "hash":
				raw[32] ^= 1
			case "padding":
				raw[len(raw)-1] = 1
			case "oversize-length":
				binary.LittleEndian.PutUint32(raw[24:28], math.MaxUint32)
			}
			got, err := storageOwnerDecodeCatalogV2(raw, r, cl)
			if err == nil || got.Version != 0 || got.Objects != nil {
				t.Fatal("malformed frame accepted/nonzero error result")
			}
		})
	}
	mutations := []struct{ name, old, new string }{{"duplicate", "\"Version\":2,", "\"Version\":2,\"Version\":2,"}, {"nested-duplicate", "\"Backend\":1,", "\"Backend\":1,\"Backend\":1,"}, {"omitted", "\"Version\":2,", ""}, {"case-alias", "\"Version\"", "\"version\""}, {"noncanonical-number", "\"Object\":11", "\"Object\":11e0"}, {"negative", "\"Object\":11", "\"Object\":-1"}, {"overflow", "\"Object\":11", "\"Object\":18446744073709551616"}, {"short-fixed-array", ownerTestID, "[1]"}, {"long-fixed-array", ownerTestID, "[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]"}, {"byte-overflow", ownerTestID, "[256,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]"}, {"null-list", "\"Entries\":[]", "\"Entries\":null"}, {"null-bytes", "\"HomePath\":\"\"", "\"HomePath\":null"}, {"byte-array", "\"HomePath\":\"\"", "\"HomePath\":[]"}, {"base64-trailing-bits", "\"L2NvbmZpZw==\"", "\"L2NvbmZpZx==\""}, {"noncanonical-escape", "\"Phase\":\"prepared\"", "\"Phase\":\"\\u0070repared\""}, {"unknown-healthy-bit", "\"Version\":2,", "\"Version\":2,\"Healthy\":true,"}, {"unknown-startup-borrow-token", "\"Version\":2,", "\"Version\":2,\"StartupHomeHeld\":true,"}, {"unknown-ready-inode", "\"Version\":2,", "\"Version\":2,\"ReadyObject\":0,"}}
	for _, v := range mutations {
		t.Run(v.name, func(t *testing.T) {
			raw := strings.Replace(payload, v.old, v.new, 1)
			if raw == payload {
				t.Fatal("fixture mutation did not apply")
			}
			if _, err := storageOwnerDecodeCatalogV2(ownerCatalogWire(raw, 1), r, cl); err == nil {
				t.Fatal("noncanonical/unknown payload accepted")
			}
		})
	}
	for _, name := range []string{"leading-space", "trailing-space", "trailing-value", "member-order", "invalid-utf8"} {
		t.Run(name, func(t *testing.T) {
			s := payload
			switch name {
			case "leading-space":
				s = " " + s
			case "trailing-space":
				s += "\n"
			case "trailing-value":
				s += "{}"
			case "member-order":
				s = strings.Replace(s, `{"Version":2,"Installation":`+ownerTestID, `{"Installation":`+ownerTestID+`,"Version":2`, 1)
			case "invalid-utf8":
				s = strings.Replace(s, "prepared", string([]byte{0xff}), 1)
			}
			if _, err := storageOwnerDecodeCatalogV2(ownerCatalogWire(s, 1), r, cl); err == nil {
				t.Fatal("noncanonical payload accepted")
			}
		})
	}
}

func TestStorageOwnerCatalogV2ObjectRoleRefusals(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*storageOwnerCatalogV2)
	}{
		{"unknown-role", func(c *storageOwnerCatalogV2) { c.Objects[20].Role = 25 }},
		{"ready-role-forbidden", func(c *storageOwnerCatalogV2) { c.Objects[20].Role = 0 }},
		{"missing-fixed-control", func(c *storageOwnerCatalogV2) { c.Objects[15].Role = 23 }},
		{"wrong-fixed-length", func(c *storageOwnerCatalogV2) { c.Objects[15].FixedLength-- }},
		{"data-marker-zero-length", func(c *storageOwnerCatalogV2) { c.Objects[19].FixedLength = 0 }},
		{"data-marker-over-bound", func(c *storageOwnerCatalogV2) { c.Objects[19].FixedLength = 4097 }},
		{"provider-identity-over-bound", func(c *storageOwnerCatalogV2) { c.Objects[22].FixedLength = 160 }},
		{"regular-parent", func(c *storageOwnerCatalogV2) { c.Objects[24].Parent = 19 }},
		{"dangling-parent", func(c *storageOwnerCatalogV2) { c.Objects[24].Parent = 9000 }},
		{"cycle", func(c *storageOwnerCatalogV2) { c.Objects[23].Parent = 23 }},
		{"duplicate-persistent-object", func(c *storageOwnerCatalogV2) { c.Objects[24].Persistent = c.Objects[23].Persistent }},
		{"unsorted-objects", func(c *storageOwnerCatalogV2) { c.Objects[0], c.Objects[1] = c.Objects[1], c.Objects[0] }},
		{"duplicate-edge", func(c *storageOwnerCatalogV2) { c.Objects[24].Parent = 0; c.Objects[24].Name = []byte("private") }},
		{"boundary-source1", func(c *storageOwnerCatalogV2) { c.Objects[20].Source = 1; c.Objects[20].OwnerClass = 1 }},
		{"boundary-name", func(c *storageOwnerCatalogV2) { c.Objects[20].Name = []byte("selected") }},
		{"boundary-owner-class", func(c *storageOwnerCatalogV2) { c.Objects[20].OwnerClass = 1 }},
		{"boundary-mode", func(c *storageOwnerCatalogV2) { c.Objects[20].ModeClass = 2 }},
		{"boundary-unbound", func(c *storageOwnerCatalogV2) { c.Roots = c.Roots[:3] }},
		{"source1-config-to-source3-role20", func(c *storageOwnerCatalogV2) { c.Objects[23].Source = 3; c.Objects[23].OwnerClass = 2 }},
		{"source1-config-to-source3-role21", func(c *storageOwnerCatalogV2) {
			c.Objects[24].Parent = 0
			c.Objects[24].Source = 3
			c.Objects[24].OwnerClass = 2
		}},
		{"source3-private-to-source1", func(c *storageOwnerCatalogV2) { c.Objects[24].Parent = 18 }},
		{"source1-private-to-source3", func(c *storageOwnerCatalogV2) { c.Objects[24].Source = 3; c.Objects[24].OwnerClass = 2 }},
		{"history-parent-forbidden", func(c *storageOwnerCatalogV2) {
			c.Objects[24].Parent = 20
			c.Objects[24].Source = 3
			c.Objects[24].OwnerClass = 2
		}},
		{"provider-parent-forbidden", func(c *storageOwnerCatalogV2) {
			c.Objects[24].Parent = 21
			c.Objects[24].Source = 3
			c.Objects[24].OwnerClass = 2
		}},
		{"source2-nonboundary", func(c *storageOwnerCatalogV2) { c.Objects[24].Source = 2 }},
		{"dot-component", func(c *storageOwnerCatalogV2) { c.Objects[24].Name = []byte(".") }},
		{"dotdot-component", func(c *storageOwnerCatalogV2) { c.Objects[24].Name = []byte("..") }},
		{"slash-component", func(c *storageOwnerCatalogV2) { c.Objects[24].Name = []byte("a/b") }},
		{"backslash-component", func(c *storageOwnerCatalogV2) { c.Objects[24].Name = []byte("a\\b") }},
		{"nul-component", func(c *storageOwnerCatalogV2) { c.Objects[24].Name = []byte{'x', 0} }},
		{"record-witness-mismatch", func(c *storageOwnerCatalogV2) { c.Objects[0].Persistent.Incarnation = 1 }},
	}
	for _, v := range mutations {
		t.Run(v.name, func(t *testing.T) {
			c, r, cl := ownerCatalogFixture()
			v.mutate(&c)
			if err := c.validate(r, cl); err == nil {
				t.Fatal("invalid object graph accepted")
			}
		})
	}
}

func TestStorageOwnerCatalogV2TypedJoinRefusals(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*storageOwnerCatalogV2)
	}{
		{"installation", func(c *storageOwnerCatalogV2) { c.Installation[0]++ }},
		{"q-binding", func(c *storageOwnerCatalogV2) { c.QBinding[0]++ }},
		{"phase", func(c *storageOwnerCatalogV2) { c.Phase = "healthy" }},
		{"transition", func(c *storageOwnerCatalogV2) { c.Transition = storagebudget.ID{} }},
		{"q-revision", func(c *storageOwnerCatalogV2) { c.ExpectedQRevision = 0 }},
		{"policy-duration", func(c *storageOwnerCatalogV2) { c.Policy.ObservationMaxAgeNanos = 3600000000001 }},
		{"policy-control-floor", func(c *storageOwnerCatalogV2) { c.Policy.Installation.Category[4].Bytes = 1 }},
		{"profile-evidence", func(c *storageOwnerCatalogV2) { c.Profiles[0].Evidence[0]++ }},
		{"membership-profile-version", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ProfileVersion = 2 }},
		{"membership-profile-id", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ProfileID = 2 }},
		{"filesystem-profile-id", func(c *storageOwnerCatalogV2) { c.Filesystems[0].PersistentProfile = 2 }},
		{"filesystem-capability", func(c *storageOwnerCatalogV2) { c.Filesystems[0].Definition.CapabilityEvidence[0]++ }},
		{"membership-unknown-subject", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.State = 0 }},
		{"membership-subject-kind", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.Group.Kind = 1 }},
		{"membership-disabled-constraint", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.Constraint = storagebudget.ID{10} }},
		{"membership-enforced-missing-row", func(c *storageOwnerCatalogV2) {
			c.Roots[0].Membership.User.State = 3
			c.Roots[0].Membership.User.Constraint = storagebudget.ID{11}
		}},
		{"membership-creation", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.Creation = 0 }},
		{"membership-existing-directory", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ExistingObjects = []uint32{0} }},
		{"membership-existing-duplicate", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ExistingObjects = []uint32{9, 9} }},
		{"membership-existing-unsorted", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ExistingObjects = []uint32{9, 8} }},
		{"membership-existing-sentinel", func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ExistingObjects = []uint32{math.MaxUint32} }},
		{"root-duplicate-directory", func(c *storageOwnerCatalogV2) { c.Roots[3].DirectoryObject = c.Roots[2].DirectoryObject }},
		{"root-wrong-kind", func(c *storageOwnerCatalogV2) { c.Roots[3].DirectoryObject = 19 }},
		{"root-domain", func(c *storageOwnerCatalogV2) { c.Roots[3].DomainObject = 17 }},
		{"root-work", func(c *storageOwnerCatalogV2) { c.Roots[3].WorkObject = 16 }},
		{"root-source-kind", func(c *storageOwnerCatalogV2) { c.Roots[3].Source.Kind = 3 }},
		{"root-category", func(c *storageOwnerCatalogV2) { c.Roots[3].Category = 1 }},
		{"root-parent-cycle", func(c *storageOwnerCatalogV2) { c.Roots[5].Source.ParentRoot = storagebudget.ID{7} }},
		{"root-parent-unknown", func(c *storageOwnerCatalogV2) { c.Roots[5].Source.ParentRoot = storagebudget.ID{99} }},
		{"root-parent-chain", func(c *storageOwnerCatalogV2) { c.Roots[6].Source.ParentRoot = storagebudget.ID{3} }},
		{"root-relative", func(c *storageOwnerCatalogV2) { c.Roots[6].Source.Relative = []byte("other") }},
		{"root-path", func(c *storageOwnerCatalogV2) { c.Roots[6].Source.Path = []byte("/elsewhere") }},
		{"root-relative-depth", func(c *storageOwnerCatalogV2) { c.Roots[2].Source.Relative = []byte(strings.Repeat("x/", 64) + "x") }},
		{"root-source-owner", func(c *storageOwnerCatalogV2) { c.Roots[2].Source.OwnerKey = []byte("arbitrary") }},
		{"root-reference-unknown", func(c *storageOwnerCatalogV2) { c.Roots[2].References = []storagebudget.ID{{99}} }},
		{"root-reference-duplicate", func(c *storageOwnerCatalogV2) { c.Roots[2].References = []storagebudget.ID{{1}, {1}} }},
		{"root-workspace-typed-join", func(c *storageOwnerCatalogV2) { c.Roots[2].Definition.Workspace = storagebudget.ID{10} }},
		{"root-filesystem-typed-join", func(c *storageOwnerCatalogV2) { c.Roots[2].Definition.Filesystem = storagebudget.ID{10} }},
		{"capacity-binding", func(c *storageOwnerCatalogV2) { c.Roots[0].Definition.CapacityBinding[0]++ }},
		{"capacity-ref-generation", func(c *storageOwnerCatalogV2) { c.Roots[0].Definition.CapacityRefs[0].Generation++ }},
		{"capacity-ref-tail", func(c *storageOwnerCatalogV2) { c.Roots[0].Definition.CapacityRefs[3].Generation = 1 }},
		{"capacity-mask", func(c *storageOwnerCatalogV2) { c.Constraints[0].Definition.Resources = 4 }},
		{"constraint-subject", func(c *storageOwnerCatalogV2) { c.Constraints[0].Subject.State = 1 }},
		{"control-owner", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].OwnerKey = []byte(strings.Repeat("a", 64)) }},
		{"control-parent", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].ParentObject = 1 }},
		{"control-name", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].Name = []byte("other.json") }},
		{"control-bound", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].MaxBytes++ }},
		{"control-location-version", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].Location.Version = 0 }},
		{"control-location-po", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].Location.Selected.Object = 999 }},
		{"control-previous-zero-pair", func(c *storageOwnerCatalogV2) {
			c.OwnerControls[0].Location.PreviousSelected = c.Objects[18].Persistent
		}},
		{"control-full-digest", func(c *storageOwnerCatalogV2) { c.OwnerControls[0].RelationDigest[0]++ }},
		{"workspace-kind", func(c *storageOwnerCatalogV2) { c.Workspaces[0].OwnerKind = 4 }},
		{"workspace-startup-key", func(c *storageOwnerCatalogV2) { c.Workspaces[0].OwnerKey = []byte("opaque") }},
		{"workspace-retained-key", func(c *storageOwnerCatalogV2) { c.Workspaces[1].OwnerKey = []byte("bad\nkey") }},
		{"workspace-history", func(c *storageOwnerCatalogV2) { c.Workspaces[1].HistoryIdentity = []byte("relative") }},
		{"before-unknown-id", func(c *storageOwnerCatalogV2) { c.BeforeDefinitionIDs.Roots = []storagebudget.ID{{99}} }},
		{"before-not-closed", func(c *storageOwnerCatalogV2) { c.BeforeDefinitionIDs.Roots = []storagebudget.ID{{1}} }},
		{"before-duplicate", func(c *storageOwnerCatalogV2) { c.BeforeDefinitionIDs.Constraints = []storagebudget.ID{{10}, {10}} }},
		{"before-digest", func(c *storageOwnerCatalogV2) { c.BeforeDefinitionsDigest[0]++ }},
		{"after-digest", func(c *storageOwnerCatalogV2) { c.AfterDefinitionsDigest[0]++ }},
	}
	for _, v := range mutations {
		t.Run(v.name, func(t *testing.T) {
			c, r, cl := ownerCatalogFixture()
			v.mutate(&c)
			switch v.name {
			case "root-parent-cycle", "root-parent-unknown", "root-parent-chain", "root-relative", "root-path":
				// These graph vectors must reach the graph validator. Recompute
				// each changed source/root/capacity/definition operand with the
				// independent handwritten material oracle, then assemble a valid
				// frame checksum independently of the production encoder.
				ownerCatalogSeal(&c, &r, &cl)
				got, err := storageOwnerDecodeCatalogV2(ownerCatalogWire(ownerCatalogJSON(c), c.Sequence), r, cl)
				if err == nil || !reflect.DeepEqual(got, storageOwnerCatalogV2{}) {
					t.Fatal("invalid root graph accepted or nonzero failure result")
				}
				return
			}
			if err := c.validate(r, cl); err == nil {
				t.Fatal("invalid typed relation accepted")
			}
		})
	}
}

func TestStorageOwnerCatalogV2CATC1Continuity(t *testing.T) {
	c, r, cl := ownerCatalogFixture()
	old := c.Roots[3].Definition
	c.Sequence = 3
	c.PreviousCatalogDigest = storagebudget.Digest{99}
	// Exact old subsets include dependencies; these are structural metadata,
	// never proof that the actual Q/previous catalog retained those rows.
	c.BeforeDefinitionIDs = storageOwnerDefinitionIDs{Roots: []storagebudget.ID{{4}}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{{1}}, Constraints: []storagebudget.ID{{10}}}
	for _, name := range []string{"current", "previous", "retained"} {
		t.Run(name, func(t *testing.T) {
			v := ownerCatalogClone(c)
			switch name {
			case "previous":
				v.OwnerControls[0].Location.Previous = []byte("/data/a")
				v.OwnerControls[0].Location.PreviousSelected = v.Objects[20].Persistent
				v.OwnerControls[0].Location.Path = []byte("/desk/private")
				v.OwnerControls[0].Location.Selected = v.Objects[18].Persistent
				v.Roots[3].Source.ControlSelection = 2
			case "retained":
				v.OwnerControls[0].Location.Path = []byte("/desk/private")
				v.OwnerControls[0].Location.Selected = v.Objects[18].Persistent
				v.Roots[3].Source.ControlSelection = 3
			}
			ownerCatalogSeal(&v, &r, &cl)
			if v.Roots[3].Definition != old {
				t.Fatal("CATC1 old immutable row changed")
			}
			if err := v.validate(r, cl); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, name := range []string{"retained-new-root", "retained-matches-current", "retained-matches-previous", "current-mismatch", "previous-mismatch", "selection-zero", "selection-unknown", "fixed-selection"} {
		t.Run(name, func(t *testing.T) {
			v := ownerCatalogClone(c)
			switch name {
			case "retained-new-root":
				v.BeforeDefinitionIDs.Roots = []storagebudget.ID{}
				v.Roots[3].Source.ControlSelection = 3
				v.OwnerControls[0].Location.Path = []byte("/desk/private")
				v.OwnerControls[0].Location.Selected = v.Objects[18].Persistent
			case "retained-matches-current":
				v.Roots[3].Source.ControlSelection = 3
			case "retained-matches-previous":
				v.Roots[3].Source.ControlSelection = 3
				v.OwnerControls[0].Location.Previous = []byte("/data/a")
				v.OwnerControls[0].Location.PreviousSelected = v.Objects[20].Persistent
			case "current-mismatch":
				v.Roots[3].Source.Path = []byte("/elsewhere")
			case "previous-mismatch":
				v.Roots[3].Source.ControlSelection = 2
			case "selection-zero":
				v.Roots[3].Source.ControlSelection = 0
			case "selection-unknown":
				v.Roots[3].Source.ControlSelection = 4
			case "fixed-selection":
				v.Roots[2].Source.ControlSelection = 1
			}
			if err := v.validateRootRole(v.Roots[3]); name == "fixed-selection" {
				if err = v.validateRootRole(v.Roots[2]); err == nil {
					t.Fatal("fixed selection accepted")
				}
			} else if err == nil {
				t.Fatal("false annotation accepted")
			}
		})
	}
	t.Run("observation-changes-selected-projection-does-not", func(t *testing.T) {
		v := ownerCatalogClone(c)
		v.OwnerControls[0].Location.Previous = []byte("/desk/private")
		v.OwnerControls[0].Location.PreviousSelected = v.Objects[18].Persistent
		observation, _ := v.controlDigest(v.OwnerControls[0])
		if observation == c.OwnerControls[0].RelationDigest {
			t.Fatal("full observation omitted Previous")
		}
		source, _ := v.sourceDigest(v.Roots[3])
		want, _ := c.sourceDigest(c.Roots[3])
		if source != want {
			t.Fatal("companion Previous leaked into immutable source")
		}
	})
	for _, name := range []string{"selected-path", "selected-po", "owner-key", "control-parent", "control-name", "domain", "membership"} {
		t.Run(name, func(t *testing.T) {
			v := ownerCatalogClone(c)
			switch name {
			case "selected-path":
				v.Roots[3].Source.Path = []byte("/changed")
			case "selected-po":
				v.Objects[20].Persistent.Incarnation = 1
			case "owner-key":
				v.Roots[3].Source.OwnerKey = []byte(strings.Repeat("a", 64))
				v.OwnerControls[0].OwnerKey = v.Roots[3].Source.OwnerKey
			case "control-parent":
				v.OwnerControls[0].ParentObject = 23
			case "control-name":
				v.OwnerControls[0].Name = []byte("other.json")
			case "domain":
				v.Roots[3].DomainObject = 17
			case "membership":
				v.Roots[3].Membership.Creation = 2
			}
			got, _ := v.rootIdentity(v.Roots[3])
			if got == old.Identity {
				t.Fatal("original authority omitted from immutable identity")
			}
		})
	}
}

func TestStorageOwnerCatalogV2DefinitionsAndOBC(t *testing.T) {
	c, r, cl := ownerCatalogFixture()
	t.Run("old-subset-must-be-dependency-closed", func(t *testing.T) {
		v := ownerCatalogClone(c)
		v.BeforeDefinitionIDs.Roots = []storagebudget.ID{{3}}
		if _, _, err := v.definitions(); err == nil {
			t.Fatal("old root accepted without old filesystem/workspace/constraint dependencies")
		}
	})
	t.Run("complete-detached-empty-before", func(t *testing.T) {
		before, after, err := c.definitions()
		if err != nil || len(before.Roots) != 0 || len(after.Roots) != 7 || before.Policy != after.Policy {
			t.Fatal("incomplete projection")
		}
		after.Roots[0].Locator = "mutated"
		if c.Roots[0].Definition.Locator == "mutated" {
			t.Fatal("definitions alias catalog")
		}
	})
	t.Run("policy-and-immutable-attribution-covered", func(t *testing.T) {
		v := ownerCatalogClone(c)
		v.Roots[0].Definition.Locator = "changed"
		if identity, _ := v.rootIdentity(v.Roots[0]); identity != c.Roots[0].Definition.Identity {
			t.Fatal("Locator entered physical identity")
		}
		if err := v.validate(r, cl); err == nil {
			t.Fatal("Locator lost definitions binding")
		}
		v = c
		v.Policy.Workspace.Total.Bytes++
		if err := v.validate(r, cl); err == nil {
			t.Fatal("policy lost definitions binding")
		}
	})
	t.Run("ready-and-startup-authority-absent", func(t *testing.T) {
		raw := ownerCatalogJSON(c)
		for _, s := range []string{"ReadyObject", "StartupHomeHeld", "Known", "Usage", "Healthy", "MountID", "FD"} {
			if strings.Contains(raw, `"`+s+`"`) {
				t.Fatal("live authority serialized")
			}
		}
	})
	t.Run("same-relation-control-replacement-has-no-inode-field", func(t *testing.T) {
		a, _ := c.sourceDigest(c.Roots[3])
		v := ownerCatalogClone(c)
		v.Sequence++
		v.Phase = "active"
		b, _ := v.sourceDigest(v.Roots[3])
		if a != b {
			t.Fatal("catalog observation entered source identity")
		}
	})
	t.Run("profile-recomputation-is-not-live-qualification", func(t *testing.T) {
		v := ownerCatalogClone(c)
		v.Profiles[0].ID = 63
		v.Filesystems[0].PersistentProfile = 63
		v.Filesystems[0].InodeProfile = 63
		for i := range v.Roots {
			v.Roots[i].Membership.ProfileID = 63
		}
		ownerCatalogSeal(&v, &r, &cl)
		if err := v.validate(r, cl); err != nil {
			t.Fatal("detached codec invented installed-table authority")
		}
	})
	t.Run("claim-record-join", func(t *testing.T) {
		cl.Nonce[0]++
		if _, err := storageOwnerDecodeCatalogV2(ownerCatalogWire(ownerCatalogJSON(c), 1), r, cl); err == nil {
			t.Fatal("foreign claim accepted")
		}
	})
}

func TestStorageOwnerCatalogV2PreallocationBounds(t *testing.T) {
	c, _, _ := ownerCatalogFixture()
	for _, name := range []string{"objects-plus-controls", "roots", "workspaces", "filesystems", "constraints", "profiles", "entries", "references-per-root", "references-aggregate", "existing-per-root", "source-bytes-aggregate", "relative-bytes", "path-bytes", "name-bytes", "owner-key-bytes", "payload-bytes", "null-empty-list"} {
		t.Run(name, func(t *testing.T) {
			v := ownerCatalogClone(c)
			switch name {
			case "objects-plus-controls":
				v.Objects = make([]storageOwnerObject, 8192)
				for i := range v.Objects {
					v.Objects[i] = c.Objects[0]
				}
			case "roots":
				v.Roots = make([]storageOwnerRoot, 4097)
				for i := range v.Roots {
					v.Roots[i] = c.Roots[0]
				}
			case "workspaces":
				v.Workspaces = make([]storageOwnerWorkspace, 1025)
				for i := range v.Workspaces {
					v.Workspaces[i] = c.Workspaces[0]
				}
			case "filesystems":
				v.Filesystems = make([]storageOwnerFilesystem, 129)
			case "constraints":
				v.Constraints = make([]storageOwnerConstraint, 513)
			case "profiles":
				v.Profiles = make([]storageOwnerProfile, 65)
			case "entries":
				v.OriginalOwner.Entries = make([]storageOwnerHomeRelation, 1025)
				for i := range v.OriginalOwner.Entries {
					v.OriginalOwner.Entries[i] = storageOwnerHomeRelation{strings.Repeat("a", 32), []byte("project"), 1, []byte("/project"), strings.Repeat("b", 64), strings.Repeat("c", 64)}
				}
			case "references-per-root":
				v.Roots[0].References = make([]storagebudget.ID, 1025)
			case "references-aggregate":
				v.Roots = make([]storageOwnerRoot, 65)
				for i := range v.Roots {
					v.Roots[i] = c.Roots[0]
					v.Roots[i].References = make([]storagebudget.ID, 1024)
				}
			case "existing-per-root":
				v.Roots[0].Membership.ExistingObjects = make([]uint32, 8193)
			case "source-bytes-aggregate":
				v.Roots = make([]storageOwnerRoot, 1025)
				for i := range v.Roots {
					v.Roots[i] = c.Roots[0]
					v.Roots[i].Source.Path = bytes.Repeat([]byte{'x'}, 4096)
				}
			case "relative-bytes":
				v.Roots[0].Source.Relative = bytes.Repeat([]byte{'x'}, 65600)
			case "path-bytes":
				v.Roots[0].Source.Path = bytes.Repeat([]byte{'x'}, 4097)
			case "name-bytes":
				v.Objects[0].Name = bytes.Repeat([]byte{'x'}, 1025)
			case "owner-key-bytes":
				v.Roots[0].Source.OwnerKey = bytes.Repeat([]byte{'x'}, 4097)
			case "payload-bytes":
				if storageOwnerCatalogPreflight(bytes.Repeat([]byte{' '}, 8<<20)) {
					t.Fatal("oversize payload preflight")
				}
				return
			case "null-empty-list":
				v.Roots[0].References = nil
			}
			raw := []byte(ownerCatalogJSON(v))
			if storageOwnerCatalogPreflight(raw) {
				t.Fatal("raw bounds/null preflight accepted")
			}
			if name != "null-empty-list" && v.bounded() {
				t.Fatal("author bounds accepted")
			}
		})
	}
}

func TestStorageOwnerCatalogV2BoundarySelectorUniqueness(t *testing.T) {
	for _, name := range []string{"duplicate-selector", "distinct-path", "distinct-complete-owner-key", "partial-owner-key", "kind-without-matching-role"} {
		t.Run(name, func(t *testing.T) {
			c, r, cl := ownerCatalogFixture()
			idx := uint32(len(c.Objects))
			c.Objects = append(c.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: storageOwnerPersistentObject{Backend: 1, VolumeUUID: [16]byte{1}, Object: 100, IncarnationKind: 1, ObjectKind: 1}, OwnerClass: 2, ModeClass: 1})
			root := c.Roots[2]
			root.Definition.ID = storagebudget.ID{8}
			root.Definition.Locator = "selected-boundary:8"
			root.DirectoryObject = idx
			root.Source.ControlKind = 0
			root.Source.ControlIndex = storageOwnerNoControl
			root.Source.ControlSelection = 0
			// The original root still selects its original fixed marker. The
			// new row has distinct PO/ID while all other typed joins are legal.
			switch name {
			case "distinct-path":
				root.Source.Path = []byte("/desk/other-private")
			case "distinct-complete-owner-key":
				root.Source.OwnerKey = []byte(strings.Repeat("e", 32))
			case "partial-owner-key":
				root.Source.OwnerKey = []byte("cccc")
			case "kind-without-matching-role":
				root.Source.Kind = 6
			}
			c.Roots = append(c.Roots, root)
			// No stale identity or checksum can mask the targeted selector
			// invariant: use the independent material oracle and frame builder.
			ownerCatalogSeal(&c, &r, &cl)
			wire := ownerCatalogWire(ownerCatalogJSON(c), c.Sequence)
			positive := name == "distinct-path" || name == "distinct-complete-owner-key"
			encoded, encodeErr := storageOwnerEncodeCatalogV2(c, r, cl)
			got, decodeErr := storageOwnerDecodeCatalogV2(wire, r, cl)
			if positive {
				if encodeErr != nil || decodeErr != nil || len(encoded) != storageOwnerCatalogBytes || !reflect.DeepEqual(got, c) {
					t.Fatalf("distinct valid selector refused: encode=%v decode=%v", encodeErr, decodeErr)
				}
			} else if encodeErr == nil || decodeErr == nil || !reflect.DeepEqual(got, storageOwnerCatalogV2{}) {
				t.Fatal("invalid selector accepted or nonzero decode failure")
			}
		})
	}
}
