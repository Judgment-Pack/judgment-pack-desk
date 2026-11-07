package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These fixtures extend the existing handwritten oracle. No serialized value
// or expected digest is recorded from a run of the implementation under test.
const ownerAdvisoryMaterialHex = "6465736b2d61647669736f72792d616c6c6f636174696f6e2d76320000000100000000000200000000000000040000000000000002000000000000000000100000000000000000200000000000000080000000006400000000000000000400000000000000f2052a0100000080b2e60e00000000"

func ownerAdvisoryPolicyRow(installation storagebudget.ID) storageOwnerConstraint {
	material, err := hex.DecodeString(ownerAdvisoryMaterialHex)
	if err != nil {
		panic(err)
	}
	domain := []byte("desk-storage-advisory-availability-v1")
	idMaterial := append(append([]byte{}, domain...), installation[:]...)
	digest := sha256.Sum256(idMaterial)
	var id storagebudget.ID
	copy(id[:], digest[:16])
	identityMaterial := append(append(append(append([]byte{}, domain...), 0), installation[:]...), material...)
	return storageOwnerConstraint{Definition: storagebudget.CapacityConstraint{ID: id, Identity: sha256.Sum256(identityMaterial), Generation: 1, Resources: 3}, Kind: 7, Pool: installation}
}
func ownerAdvisoryRefs(c *storageOwnerCatalogV2) {
	sort.Slice(c.Constraints, func(i, j int) bool {
		return bytes.Compare(c.Constraints[i].Definition.ID[:], c.Constraints[j].Definition.ID[:]) < 0
	})
	// All handmade fixtures in this file have one observation scope and the same
	// applicable native rows on every root. Production never derives refs this way.
	for i := range c.Roots {
		r := &c.Roots[i]
		r.Definition.CapacityCount = uint8(len(c.Constraints))
		r.Definition.CapacityRefs = [4]storagebudget.CapacityRef{}
		for j, v := range c.Constraints {
			r.Definition.CapacityRefs[j] = storagebudget.CapacityRef{Constraint: v.Definition.ID, Generation: v.Definition.Generation}
		}
	}
}
func ownerAdvisoryFixture(apfs bool) (storageOwnerCatalogV2, storageOwnerRecordV2, storageOwnerClaimV2) {
	c, r, cl := ownerCatalogFixture()
	if apfs {
		ownerCatalogAPFS(&c, &r)
	}
	c.Version = 3
	p := &c.Profiles[0]
	p.ID = 4097
	if apfs {
		p.ID = 4098
	}
	p.Version = 2
	p.MappingEvidence = storagebudget.Digest{}
	p.QuotaEvidence = storagebudget.Digest{}
	material, _ := hex.DecodeString(ownerAdvisoryMaterialHex)
	p.AllowanceEvidence = sha256.Sum256(material)
	policy := ownerAdvisoryPolicyRow(c.Installation)
	c.Constraints = []storageOwnerConstraint{policy}
	f := &c.Filesystems[0]
	f.PersistentProfile = p.ID
	f.InodeProfile = p.ID
	f.Definition.ByteConstraint = policy.Definition.ID
	f.Definition.InodeConstraint = policy.Definition.ID
	if apfs {
		f.ContainerUUID = [16]byte{}
		f.Definition.InodeConstraint = storagebudget.ID{}
	}
	for i := range c.Roots {
		m := &c.Roots[i].Membership
		m.ProfileID = p.ID
		m.ProfileVersion = 2
		m.Pool = f.Definition.ID
		if apfs {
			m.ContainerUUID = [16]byte{}
		} else {
			m.User = storageOwnerSubject{State: 4, Kind: 1}
			m.Group = storageOwnerSubject{State: 4, Kind: 2}
			m.Project = storageOwnerSubject{State: 4, Kind: 3}
		}
	}
	ownerAdvisoryRefs(&c)
	ownerCatalogSeal(&c, &r, &cl)
	return c, r, cl
}
func ownerAdvisoryWire(c storageOwnerCatalogV2) []byte {
	raw := ownerCatalogWire(ownerCatalogJSON(c), c.Sequence)
	raw[6] = '3'
	raw[8] = 3
	ownerTestChecksum(raw)
	return raw
}
func TestStorageOwnerCatalogV3HandwrittenEncoding(t *testing.T) {
	for _, apfs := range []bool{false, true} {
		c, r, cl := ownerAdvisoryFixture(apfs)
		raw, err := storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(c), r, cl)
		if err != nil {
			t.Fatal(apfs, err)
		}
		if !bytes.Equal(raw, ownerAdvisoryWire(c)) {
			t.Fatal("v3 framing differs from handwritten oracle")
		}
		decoded, err := storageOwnerDecodeCatalogV3(raw, r, cl)
		if err != nil || !reflect.DeepEqual(c, storageOwnerCatalogV2(decoded)) {
			t.Fatal("v3 round trip", err)
		}
		if _, err = storageOwnerDecodeCatalogV2(raw, r, cl); err == nil {
			t.Fatal("v2 accepted v3 frame")
		}
		// A freshly checksummed v2 envelope must not reinterpret v3 semantics.
		if _, err = storageOwnerDecodeCatalogV2(ownerCatalogWire(ownerCatalogJSON(c), 1), r, cl); err == nil {
			t.Fatal("v2 accepted v3 payload")
		}
	}
	c, r, cl := ownerCatalogFixture()
	raw, err := storageOwnerEncodeCatalogV2(c, r, cl)
	if err != nil || !bytes.Equal(raw, ownerCatalogWire(ownerCatalogJSON(c), 1)) {
		t.Fatal("v2 bytes changed", err)
	}
	if _, err = storageOwnerDecodeCatalogV3(raw, r, cl); err == nil {
		t.Fatal("v3 accepted v2 frame")
	}
	if _, err = storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(c), r, cl); err == nil {
		t.Fatal("v3 accepted v2 payload")
	}
	// New enum meanings remain invalid even when explicitly labelled Version2.
	c, r, cl = ownerAdvisoryFixture(false)
	c.Version = 2
	ownerCatalogSeal(&c, &r, &cl)
	if _, err = storageOwnerDecodeCatalogV2(ownerCatalogWire(ownerCatalogJSON(c), 1), r, cl); err == nil {
		t.Fatal("v2 accepted new advisory meanings")
	}
}
func TestStorageOwnerCatalogV3PolicyIdentity(t *testing.T) {
	c, _, _ := ownerAdvisoryFixture(false)
	want := ownerAdvisoryPolicyRow(c.Installation)
	if got := storageOwnerAdvisoryConstraint(c.Installation); got != want {
		t.Fatal("policy material/domain/identity mismatch")
	}
	if storageOwnerAdvisoryConstraint(storagebudget.ID{2}).Definition.ID == want.Definition.ID {
		t.Fatal("policy id omitted installation")
	}
}
func TestStorageOwnerCatalogV3NativeRows(t *testing.T) {
	for _, name := range []string{"linux-known-three-classes", "linux-positive-disabled-accounting", "apfs-known-container", "apfs-known-container-volume"} {
		t.Run(name, func(t *testing.T) {
			apfs := name == "apfs-known-container" || name == "apfs-known-container-volume"
			c, r, cl := ownerAdvisoryFixture(apfs)
			c.Profiles[0].MappingEvidence = storagebudget.Digest{2}
			c.Profiles[0].QuotaEvidence = storagebudget.Digest{3}
			if !apfs {
				if name == "linux-known-three-classes" {
					ownerCatalogQuota(&c)
				} else {
					for i := range c.Roots {
						c.Roots[i].Membership.User = storageOwnerSubject{State: 1, Kind: 1, NamespaceMap: storagebudget.Digest{2}}
						c.Roots[i].Membership.Group = storageOwnerSubject{State: 2, Kind: 2, NamespaceMap: storagebudget.Digest{2}}
					}
				}
			} else {
				c.Filesystems[0].ContainerUUID = [16]byte{2}
				for i := range c.Roots {
					c.Roots[i].Membership.ContainerUUID = [16]byte{2}
				}
				c.Constraints = append(c.Constraints, storageOwnerConstraint{Definition: storagebudget.CapacityConstraint{ID: storagebudget.ID{10}, Identity: storagebudget.Digest{10}, Generation: 1, Resources: 1}, Kind: 5, Pool: storagebudget.ID{10}, ContainerUUID: [16]byte{2}})
				if name == "apfs-known-container-volume" {
					c.Constraints = append(c.Constraints, storageOwnerConstraint{Definition: storagebudget.CapacityConstraint{ID: storagebudget.ID{11}, Identity: storagebudget.Digest{11}, Generation: 1, Resources: 1}, Kind: 6, Pool: storagebudget.ID{10}, VolumeUUID: [16]byte{1}, ContainerUUID: [16]byte{2}})
				}
			}
			ownerAdvisoryRefs(&c)
			ownerCatalogSeal(&c, &r, &cl)
			if _, err := storageOwnerDecodeCatalogV3(ownerAdvisoryWire(c), r, cl); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestStorageOwnerCatalogV3SemanticRefusals(t *testing.T) {
	tests := []struct {
		name   string
		apfs   bool
		mutate func(*storageOwnerCatalogV2)
	}{
		{"unknown-subject", false, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.Subject = 7 }},
		{"unknown-map", false, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.NamespaceMap[0] = 1 }},
		{"unknown-ref", false, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.Constraint[0] = 1 }},
		{"unknown-wrong-kind", false, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.Kind = 2 }},
		{"positive-claim-without-evidence", false, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User.State = 1 }},
		{"old-profile-version", false, func(c *storageOwnerCatalogV2) {
			c.Profiles[0].Version = 1
			for i := range c.Roots {
				c.Roots[i].Membership.ProfileVersion = 1
			}
		}},
		{"unrecognized-profile-id", false, func(c *storageOwnerCatalogV2) {
			c.Profiles[0].ID = 4100
			c.Filesystems[0].PersistentProfile = 4100
			c.Filesystems[0].InodeProfile = 4100
			for i := range c.Roots {
				c.Roots[i].Membership.ProfileID = 4100
			}
		}},
		{"wrong-policy-material", false, func(c *storageOwnerCatalogV2) { c.Profiles[0].AllowanceEvidence[0]++ }},
		{"no-counter-route-evidence", false, func(c *storageOwnerCatalogV2) { c.Profiles[0].CounterEvidence = storagebudget.Digest{} }},
		{"policy-headroom-double-applied", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].Definition.Headroom.Bytes = 1 }},
		{"policy-missing-inode-axis", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].Definition.Resources = 1 }},
		{"policy-reminted-generation", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].Definition.Generation = 2; ownerAdvisoryRefs(c) }},
		{"policy-identity", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].Definition.Identity[0]++ }},
		{"policy-pool", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].Pool[0]++ }},
		{"policy-native-volume", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].VolumeUUID[0] = 1 }},
		{"missing-policy", false, func(c *storageOwnerCatalogV2) { c.Constraints = []storageOwnerConstraint{} }},
		{"old-backing-row", false, func(c *storageOwnerCatalogV2) { c.Constraints[0].Kind = 1 }},
		{"inode-model-unknown", false, func(c *storageOwnerCatalogV2) { c.Filesystems[0].Definition.InodeModel = 0 }},
		{"inode-axis-omitted", false, func(c *storageOwnerCatalogV2) { c.Filesystems[0].Definition.InodeConstraint = storagebudget.ID{} }},
		{"stale-policy-config", false, func(c *storageOwnerCatalogV2) { c.Policy.ObservationMaxAgeNanos = 5000000001 }},
		{"apfs-invents-policy-pool", true, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.Pool = c.Constraints[0].Definition.ID }},
		{"apfs-container-mismatch", true, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.ContainerUUID[0] = 1 }},
		{"apfs-quota-unknown-is-not-applicable", true, func(c *storageOwnerCatalogV2) { c.Roots[0].Membership.User = storageOwnerSubject{State: 4, Kind: 1} }},
		{"apfs-dynamic-with-native-inode-ref", true, func(c *storageOwnerCatalogV2) {
			c.Filesystems[0].Definition.InodeConstraint = c.Constraints[0].Definition.ID
		}},
		{"apfs-orphan-volume", true, func(c *storageOwnerCatalogV2) {
			c.Constraints = append(c.Constraints, storageOwnerConstraint{Definition: storagebudget.CapacityConstraint{ID: storagebudget.ID{11}, Identity: storagebudget.Digest{11}, Generation: 1, Resources: 1}, Kind: 6, Pool: storagebudget.ID{10}, VolumeUUID: [16]byte{1}, ContainerUUID: [16]byte{2}})
			ownerAdvisoryRefs(c)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, r, cl := ownerAdvisoryFixture(tt.apfs)
			tt.mutate(&c)
			ownerCatalogSeal(&c, &r, &cl)
			if _, err := storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(c), r, cl); err == nil {
				t.Fatal("encode accepted malformed semantics")
			}
			if _, err := storageOwnerDecodeCatalogV3(ownerAdvisoryWire(c), r, cl); err == nil {
				t.Fatal("decode accepted malformed semantics")
			}
		})
	}
}
func TestStorageOwnerCatalogV3ClosedBoundsAndFrame(t *testing.T) {
	c, r, cl := ownerAdvisoryFixture(false)
	valid := ownerAdvisoryWire(c)
	for _, name := range []string{"trailing-padding", "frame-version", "reserved", "digest", "unknown-field", "duplicate-field", "null-list", "root-count", "payload-version"} {
		t.Run(name, func(t *testing.T) {
			raw := append([]byte{}, valid...)
			switch name {
			case "trailing-padding":
				raw[len(raw)-1] = 1
			case "frame-version":
				raw[8] = 2
				ownerTestChecksum(raw)
			case "reserved":
				raw[28] = 1
				ownerTestChecksum(raw)
			case "digest":
				raw[32] ^= 1
			case "unknown-field":
				payload := ownerCatalogJSON(c)
				raw = ownerCatalogWire(payload[:len(payload)-1]+`,"Surprise":0}`, 1)
				raw[6] = '3'
				raw[8] = 3
				ownerTestChecksum(raw)
			case "duplicate-field":
				payload := ownerCatalogJSON(c)
				raw = ownerCatalogWire(payload[:len(payload)-1]+`,"Version":3}`, 1)
				raw[6] = '3'
				raw[8] = 3
				ownerTestChecksum(raw)
			case "null-list":
				v := ownerCatalogClone(c)
				v.Roots = nil
				raw = ownerAdvisoryWire(v)
			case "root-count":
				v := ownerCatalogClone(c)
				v.Roots = make([]storageOwnerRoot, 4097)
				raw = ownerAdvisoryWire(v)
			case "payload-version":
				v := ownerCatalogClone(c)
				v.Version = 2
				raw = ownerAdvisoryWire(v)
			}
			if _, err := storageOwnerDecodeCatalogV3(raw, r, cl); err == nil {
				t.Fatal("accepted bad frame/bounds")
			}
		})
	}
}
func TestStorageOwnerCatalogV3Continuity(t *testing.T) {
	c, r, cl := ownerAdvisoryFixture(false)
	// Qualified profile evidence does not make any root's subject known.
	// Keep this profile and its owner binding fixed across every frame.
	c.Profiles[0].MappingEvidence = storagebudget.Digest{2}
	c.Profiles[0].QuotaEvidence = storagebudget.Digest{3}
	ownerCatalogSeal(&c, &r, &cl)
	first := ownerAdvisoryWire(c)
	active := ownerCatalogClone(c)
	active.Sequence = 2
	active.Phase = "active"
	ownerCatalogLink(&active, first)
	second := ownerAdvisoryWire(active)
	pair, err := storageOwnerDecodeCatalogPairV3(first, second, r, cl)
	if err != nil || !pair.PeerLinked {
		t.Fatal("v3 finalization", err)
	}
	if _, err = storageOwnerDecodeCatalogPairV2(first, second, r, cl); err == nil {
		t.Fatal("v2 pair accepted v3")
	}
	pair, err = storageOwnerDecodeCatalogPairV3(second, second, r, cl)
	if err != nil || !pair.Duplicated || pair.PeerLinked {
		t.Fatal("v3 duplicated frame", err)
	}
	next := ownerCatalogClone(active)
	next.Sequence = 3
	next.Phase = "prepared"
	next.Transition = storagebudget.ID{8}
	ownerCatalogLink(&next, second)
	ownerCatalogBeforeAll(&next)
	ownerCatalogSeal(&next, &r, &cl)
	if _, err = storageOwnerDecodeCatalogPairV3(second, ownerAdvisoryWire(next), r, cl); err != nil {
		t.Fatal("unchanged advisory reopen tuple", err)
	}
	next.PreviousCatalogDigest = sha256.Sum256(second) // entire-frame digest is the wrong operand
	if _, err = storageOwnerDecodeCatalogPairV3(second, ownerAdvisoryWire(next), r, cl); err == nil {
		t.Fatal("wrong predecessor operand")
	}
	// Positively discovered restrictions cannot promote an existing Unknown root
	// through append/enrollment; its immutable identity/refs/binding must survive.
	ownerCatalogLink(&next, second)
	ownerCatalogQuota(&next)
	ownerAdvisoryRefs(&next)
	// Existing roots must have a closed before tuple to reach pair continuity.
	ownerCatalogBeforeAll(&next)
	nextRecord, nextClaim := r, cl
	ownerCatalogSeal(&next, &nextRecord, &nextClaim)
	if !reflect.DeepEqual(nextRecord, r) || !reflect.DeepEqual(nextClaim, cl) {
		t.Fatal("promotion fixture changed owner record or claim")
	}
	third := ownerAdvisoryWire(next)
	if decoded, err := storageOwnerDecodeCatalogV3(second, r, cl); err != nil || !reflect.DeepEqual(storageOwnerCatalogV2(decoded), active) {
		t.Fatal("old Unknown frame must individually decode", err)
	}
	if decoded, err := storageOwnerDecodeCatalogV3(third, r, cl); err != nil || !reflect.DeepEqual(storageOwnerCatalogV2(decoded), next) {
		t.Fatal("promotion frame must individually decode", err)
	}
	pair, err = storageOwnerDecodeCatalogPairV3(second, third, r, cl)
	if err == nil || !reflect.DeepEqual(pair, storageOwnerCatalogPair{}) {
		t.Fatal("append reinterpreted existing Unknown roots or returned partial pair", err)
	}
}
func TestStoragePhysicalStatusAdvisory(t *testing.T) {
	if (storagePhysicalStatus{}).validAdvisory() {
		t.Fatal("zero status implied healthy/advisory")
	}
	s := storagePhysicalStatus{PolicyID: "desk-advisory-allocation-v2", PolicyVersion: 2, Mode: storagePhysicalAdvisory, Bytes: storagePhysicalCounterMeasured, Inodes: storagePhysicalCounterDynamic, Oldest: time.Unix(1, 0), Reason: storagePhysicalReasonDetailsUnknown}
	if !s.validAdvisory() || s.UserQuota != storagePhysicalDiscoveryUnknown || s.APFSContainer != storagePhysicalDiscoveryUnknown {
		t.Fatal("advisory status lost unknown details")
	}
	for _, mutate := range []func(*storagePhysicalStatus){func(s *storagePhysicalStatus) { s.PolicyVersion = 1 }, func(s *storagePhysicalStatus) { s.Mode = 0 }, func(s *storagePhysicalStatus) { s.Bytes = storagePhysicalCounterDynamic }, func(s *storagePhysicalStatus) { s.UserQuota = 99 }, func(s *storagePhysicalStatus) { s.Reason = 99 }, func(s *storagePhysicalStatus) { s.Oldest = time.Time{} }} {
		v := s
		mutate(&v)
		if v.validAdvisory() {
			t.Fatal("invalid advisory status accepted")
		}
	}
}
