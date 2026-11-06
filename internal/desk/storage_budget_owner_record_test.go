package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Hand-authored JSON, independent of the codec's marshaler/material projection.
const ownerTestID = `[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`
const ownerTestNonce = `[2,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`
const ownerTestDigest = `[1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`
const ownerTestHome = `0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef`
const ownerTestConfig = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":11,"Incarnation":0,"IncarnationKind":1,"ObjectKind":1}`
const ownerTestAnchor = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":12,"Incarnation":0,"IncarnationKind":1,"ObjectKind":1}`
const ownerTestBootstrap = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":13,"Incarnation":0,"IncarnationKind":1,"ObjectKind":2}`
const ownerTestUse = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":14,"Incarnation":0,"IncarnationKind":1,"ObjectKind":2}`
const ownerTestLease = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":15,"Incarnation":0,"IncarnationKind":1,"ObjectKind":2}`
const ownerTestA = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":16,"Incarnation":0,"IncarnationKind":1,"ObjectKind":2}`
const ownerTestB = `{"Backend":1,"VolumeUUID":` + ownerTestID + `,"Object":17,"Incarnation":0,"IncarnationKind":1,"ObjectKind":2}`
const ownerTestPrefix = `{"Version":2,"Kind":"installation","Installation":` + ownerTestID + `,"Nonce":` + ownerTestNonce + `,"HomeOwner":"` + ownerTestHome + `","BindingVersion":2`
const ownerTestSuffix = `,"Config":` + ownerTestConfig + `,"Anchor":` + ownerTestAnchor + `,"BootstrapLock":` + ownerTestBootstrap + `,"UseLock":` + ownerTestUse + `,"HomeLease":` + ownerTestLease + `,"CatalogA":` + ownerTestA + `,"CatalogB":` + ownerTestB + `,"ProfileSetDigest":` + ownerTestDigest + `}`
const ownerTestMaterial = ownerTestPrefix + ownerTestSuffix
const ownerTestClaim = `{"Version":2,"Kind":"claim","Installation":` + ownerTestID + `,"Nonce":` + ownerTestNonce + `,"HomeOwner":"` + ownerTestHome + `","Config":` + ownerTestConfig + `,"ProfileSetDigest":` + ownerTestDigest + `}`

func ownerTestArray(b []byte) string {
	var s strings.Builder
	s.WriteByte('[')
	for i, v := range b {
		if i != 0 {
			s.WriteByte(',')
		}
		fmt.Fprint(&s, v)
	}
	s.WriteByte(']')
	return s.String()
}
func ownerTestRecord() (storageOwnerRecordV2, string) {
	r := storageOwnerRecordV2{Version: 2, Kind: "installation", Installation: storagebudget.ID{1}, Nonce: storagebudget.ID{2}, HomeOwner: ownerTestHome, BindingVersion: 2, ProfileSetDigest: storagebudget.Digest{1}}
	object := func(n uint64, k uint8) storageOwnerPersistentObject {
		return storageOwnerPersistentObject{Backend: 1, VolumeUUID: [16]byte{1}, Object: n, IncarnationKind: 1, ObjectKind: k}
	}
	r.Config = object(11, 1)
	r.Anchor = object(12, 1)
	r.BootstrapLock = object(13, 2)
	r.UseLock = object(14, 2)
	r.HomeLease = object(15, 2)
	r.CatalogA = object(16, 2)
	r.CatalogB = object(17, 2)
	r.BindingMaterialDigest = sha256.Sum256([]byte(ownerTestMaterial))
	r.QBinding = sha256.Sum256(append([]byte("desk-storage-installation-v2\x00"), []byte(ownerTestMaterial)...))
	payload := ownerTestPrefix + `,"QBinding":` + ownerTestArray(r.QBinding[:]) + `,"BindingMaterialDigest":` + ownerTestArray(r.BindingMaterialDigest[:]) + ownerTestSuffix
	return r, payload
}
func ownerTestClaimValue() storageOwnerClaimV2 {
	return storageOwnerClaimV2{Version: 2, Kind: "claim", Installation: storagebudget.ID{1}, Nonce: storagebudget.ID{2}, HomeOwner: ownerTestHome, Config: storageOwnerPersistentObject{Backend: 1, VolumeUUID: [16]byte{1}, Object: 11, IncarnationKind: 1, ObjectKind: 1}, ProfileSetDigest: storagebudget.Digest{1}}
}

// Assemble malformed wire inputs independently: a valid checksum must not mask
// a malformed payload, sequence, reserved field or padding byte.
func ownerTestWire(payload string, claim bool) []byte {
	raw := make([]byte, 4096)
	magic := "DSKINS2\x00"
	if claim {
		magic = "DSKCLM2\x00"
	}
	copy(raw, magic)
	raw[8] = 2
	raw[12] = 64
	raw[16] = 1
	binary.LittleEndian.PutUint32(raw[24:28], uint32(len(payload)))
	copy(raw[64:], payload)
	ownerTestChecksum(raw)
	return raw
}
func ownerTestChecksum(raw []byte) {
	n := int(binary.LittleEndian.Uint32(raw[24:28]))
	input := append([]byte(nil), raw[:32]...)
	input = append(input, raw[64:64+n]...)
	sum := sha256.Sum256(input)
	copy(raw[32:64], sum[:])
}

func TestStorageOwnerV2HandwrittenEncoding(t *testing.T) {
	r, payload := ownerTestRecord()
	t.Run("record-fixed-header-payload-padding", func(t *testing.T) {
		raw, err := storageOwnerEncodeRecordV2(r)
		if err != nil {
			t.Fatal(err)
		}
		want := ownerTestWire(payload, false)
		if !bytes.Equal(raw, want) {
			t.Fatal("wire differs from handwritten record")
		}
		got, err := storageOwnerDecodeRecordV2(want)
		if err != nil || got != r {
			t.Fatalf("record: %v", err)
		}
		// The returned DTO is detached and preserves the stored verified binding.
		clear(want)
		if got.QBinding != r.QBinding || got.Config != r.Config {
			t.Fatal("decoded record aliases input")
		}
	})
	t.Run("claim-fixed-header-payload-padding", func(t *testing.T) {
		c := ownerTestClaimValue()
		raw, err := storageOwnerEncodeClaimV2(c)
		if err != nil {
			t.Fatal(err)
		}
		want := ownerTestWire(ownerTestClaim, true)
		if !bytes.Equal(raw, want) {
			t.Fatal("wire differs from handwritten claim")
		}
		got, err := storageOwnerDecodeClaimV2(want)
		if err != nil || got != c || !got.matches(r) {
			t.Fatalf("claim: %v", err)
		}
	})
	t.Run("self-hash-fields-are-excluded-but-verified", func(t *testing.T) {
		m, q, err := storageOwnerRecordDigests(r)
		if err != nil || m != r.BindingMaterialDigest || q != r.QBinding {
			t.Fatal("handwritten material/domain mismatch")
		}
		changed := r
		changed.QBinding = storagebudget.Digest{99}
		changed.BindingMaterialDigest = storagebudget.Digest{98}
		m2, q2, err := storageOwnerRecordDigests(changed)
		if err != nil || m2 != m || q2 != q {
			t.Fatal("self-hash cycle")
		}
		if _, err := storageOwnerEncodeRecordV2(changed); err == nil {
			t.Fatal("encoder repaired a stored binding")
		}
		changed = r
		changed.Nonce[1] = 1
		if _, err := storageOwnerEncodeRecordV2(changed); err == nil {
			t.Fatal("changed material retained old binding")
		}
	})
}

func TestStorageOwnerV2EnvelopeRefusals(t *testing.T) {
	_, payload := ownerTestRecord()
	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"short", func(b []byte) []byte { return b[:4095] }},
		{"long", func(b []byte) []byte { return append(b, 0) }},
		{"prototype-magic", func(b []byte) []byte { b[6] = '1'; ownerTestChecksum(b); return b }},
		{"unknown-version", func(b []byte) []byte { b[8] = 3; ownerTestChecksum(b); return b }},
		{"bad-header-size", func(b []byte) []byte { b[12] = 63; ownerTestChecksum(b); return b }},
		{"zero-sequence", func(b []byte) []byte { clear(b[16:24]); ownerTestChecksum(b); return b }},
		{"immutable-sequence-two", func(b []byte) []byte { b[16] = 2; ownerTestChecksum(b); return b }},
		{"payload-length-overflow", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[24:28], math.MaxUint32); return b }},
		{"reserved-with-valid-checksum", func(b []byte) []byte { b[28] = 1; ownerTestChecksum(b); return b }},
		{"checksum", func(b []byte) []byte { b[32] ^= 1; return b }},
		{"tampered-payload", func(b []byte) []byte { b[64] ^= 1; return b }},
		{"padding", func(b []byte) []byte { b[4095] = 1; return b }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := storageOwnerDecodeRecordV2(tc.mutate(ownerTestWire(payload, false))); err == nil || got != (storageOwnerRecordV2{}) {
				t.Fatal("malformed record accepted or nonzero failure result")
			}
			if got, err := storageOwnerDecodeClaimV2(tc.mutate(ownerTestWire(ownerTestClaim, true))); err == nil || got != (storageOwnerClaimV2{}) {
				t.Fatal("malformed claim accepted or nonzero failure result")
			}
		})
	}
	t.Run("wrong-frame-kind", func(t *testing.T) {
		if _, err := storageOwnerDecodeClaimV2(ownerTestWire(payload, false)); err == nil {
			t.Fatal("record frame became claim")
		}
		if _, err := storageOwnerDecodeRecordV2(ownerTestWire(ownerTestClaim, true)); err == nil {
			t.Fatal("claim frame became record")
		}
	})
}

func TestStorageOwnerV2ClosedCanonicalJSON(t *testing.T) {
	cases := []struct {
		name   string
		change func(string) string
	}{
		{"duplicate-member", func(s string) string { return strings.Replace(s, `"Version":2,`, `"Version":2,"Version":2,`, 1) }},
		{"duplicate-nested-member", func(s string) string { return strings.Replace(s, `"Backend":1,`, `"Backend":1,"Backend":1,`, 1) }},
		{"unknown-live-token", func(s string) string { return strings.TrimSuffix(s, "}") + `,"StartupHomeHeld":true}` }},
		{"omitted-member", func(s string) string { return strings.Replace(s, `"Version":2,`, "", 1) }},
		{"case-alias", func(s string) string { return strings.Replace(s, `"Version"`, `"version"`, 1) }},
		{"leading-whitespace", func(s string) string { return " " + s }},
		{"trailing-whitespace", func(s string) string { return s + "\n" }},
		{"trailing-value", func(s string) string { return s + "{}" }},
		{"member-order", func(s string) string {
			if strings.Contains(s, `"Kind":"claim"`) {
				return strings.Replace(s, `"Version":2,"Kind":"claim"`, `"Kind":"claim","Version":2`, 1)
			}
			return strings.Replace(s, `"Version":2,"Kind":"installation"`, `"Kind":"installation","Version":2`, 1)
		}},
		{"escaped-text", func(s string) string { return strings.Replace(s, `"HomeOwner":"0`, `"HomeOwner":"\u0030`, 1) }},
		{"invalid-utf8", func(s string) string { return strings.Replace(s, "0123456789abcdef", string([]byte{0xff}), 1) }},
		{"surrogate", func(s string) string { return strings.Replace(s, "0123456789abcdef", `\ud800`, 1) }},
		{"short-id", func(s string) string { return strings.Replace(s, ownerTestID, `[1]`, 1) }},
		{"long-id", func(s string) string {
			return strings.Replace(s, ownerTestID, strings.TrimSuffix(ownerTestID, "]")+`,0]`, 1)
		}},
		{"byte-overflow", func(s string) string {
			return strings.Replace(s, ownerTestID, `[256,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]`, 1)
		}},
		{"null-id", func(s string) string { return strings.Replace(s, ownerTestID, `null`, 1) }},
		{"uint-overflow", func(s string) string { return strings.Replace(s, `"Object":11`, `"Object":18446744073709551616`, 1) }},
		{"negative", func(s string) string { return strings.Replace(s, `"Object":11`, `"Object":-1`, 1) }},
		{"exponent", func(s string) string { return strings.Replace(s, `"Object":11`, `"Object":11e0`, 1) }},
		{"fraction", func(s string) string { return strings.Replace(s, `"Object":11`, `"Object":11.0`, 1) }},
		{"leading-zero", func(s string) string { return strings.Replace(s, `"Object":11`, `"Object":011`, 1) }},
	}
	_, payload := ownerTestRecord()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := storageOwnerDecodeRecordV2(ownerTestWire(tc.change(payload), false)); err == nil {
				t.Fatal("malformed/noncanonical record accepted")
			}
			if _, err := storageOwnerDecodeClaimV2(ownerTestWire(tc.change(ownerTestClaim), true)); err == nil {
				t.Fatal("malformed/noncanonical claim accepted")
			}
		})
	}
}

func TestStorageOwnerV2PersistentTaggedUnion(t *testing.T) {
	cases := []struct {
		name  string
		value storageOwnerPersistentObject
		kind  uint8
		valid bool
	}{
		{"ext4-zero-generation", storageOwnerPersistentObject{1, [16]byte{1}, 1, 0, 1, 1}, 1, true},
		{"ext4-max-generation", storageOwnerPersistentObject{1, [16]byte{1}, 1, math.MaxUint32, 1, 2}, 2, true},
		{"apfs-directory", storageOwnerPersistentObject{2, [16]byte{1}, 1, 2, 2, 1}, 1, true},
		{"apfs-file", storageOwnerPersistentObject{2, [16]byte{1}, 1, math.MaxUint64, 2, 2}, 2, true},
		{"ext4-generation-overflow", storageOwnerPersistentObject{1, [16]byte{1}, 1, uint64(math.MaxUint32) + 1, 1, 1}, 1, false},
		{"ext4-wrong-incarnation-tag", storageOwnerPersistentObject{1, [16]byte{1}, 1, 2, 2, 1}, 1, false},
		{"apfs-wrong-incarnation-tag", storageOwnerPersistentObject{2, [16]byte{1}, 1, 2, 1, 1}, 1, false},
		{"apfs-zero-link", storageOwnerPersistentObject{2, [16]byte{1}, 1, 0, 2, 1}, 1, false},
		{"unknown-backend", storageOwnerPersistentObject{3, [16]byte{1}, 1, 2, 1, 1}, 1, false},
		{"zero-volume", storageOwnerPersistentObject{1, [16]byte{}, 1, 2, 1, 1}, 1, false},
		{"zero-object", storageOwnerPersistentObject{1, [16]byte{1}, 0, 2, 1, 1}, 1, false},
		{"wrong-object-kind", storageOwnerPersistentObject{1, [16]byte{1}, 1, 2, 1, 2}, 1, false},
		{"unknown-object-kind", storageOwnerPersistentObject{1, [16]byte{1}, 1, 2, 1, 3}, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.valid(tc.kind); got != tc.valid {
				t.Fatalf("valid=%v, want %v", got, tc.valid)
			}
		})
	}
}

func TestStorageOwnerV2ClaimBindingAndBounds(t *testing.T) {
	r, _ := ownerTestRecord()
	mutations := []struct {
		name   string
		change func(*storageOwnerClaimV2)
	}{
		{"version", func(c *storageOwnerClaimV2) { c.Version = 1 }},
		{"kind", func(c *storageOwnerClaimV2) { c.Kind = "installation" }},
		{"zero-installation", func(c *storageOwnerClaimV2) { c.Installation = storagebudget.ID{} }},
		{"foreign-installation", func(c *storageOwnerClaimV2) { c.Installation[0] = 3 }},
		{"zero-nonce", func(c *storageOwnerClaimV2) { c.Nonce = storagebudget.ID{} }},
		{"foreign-nonce", func(c *storageOwnerClaimV2) { c.Nonce[0] = 3 }},
		{"owner", func(c *storageOwnerClaimV2) { c.HomeOwner = strings.Repeat("a", 64) }},
		{"owner-upper", func(c *storageOwnerClaimV2) { c.HomeOwner = strings.Repeat("A", 64) }},
		{"owner-short", func(c *storageOwnerClaimV2) { c.HomeOwner = "abc" }},
		{"config", func(c *storageOwnerClaimV2) { c.Config.Object = 99 }},
		{"profile-set", func(c *storageOwnerClaimV2) { c.ProfileSetDigest[1] = 1 }},
		{"profile-set-zero", func(c *storageOwnerClaimV2) { c.ProfileSetDigest = storagebudget.Digest{} }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			c := ownerTestClaimValue()
			tc.change(&c)
			if c.matches(r) {
				t.Fatal("foreign/malformed claim matched record")
			}
		})
	}
	t.Run("payload-bounds", func(t *testing.T) {
		if _, err := storageOwnerEncodeFrame(storageOwnerRecordMagic, 4096, 1, make([]byte, 4032)); err != nil {
			t.Fatal(err)
		}
		if _, err := storageOwnerEncodeFrame(storageOwnerRecordMagic, 4096, 1, make([]byte, 4033)); err == nil {
			t.Fatal("oversize payload accepted")
		}
		if _, err := storageOwnerEncodeFrame(storageOwnerRecordMagic, 8192, 1, nil); err == nil {
			t.Fatal("non-fixed frame size accepted")
		}
		if _, err := storageOwnerEncodeFrame(storageOwnerRecordMagic, 4096, 0, nil); err == nil {
			t.Fatal("zero sequence accepted")
		}
	})
}

func TestStorageOwnerV2RecordStructuralRefusals(t *testing.T) {
	mutations := []struct {
		name   string
		change func(*storageOwnerRecordV2)
	}{
		{"version", func(r *storageOwnerRecordV2) { r.Version = 1 }},
		{"kind", func(r *storageOwnerRecordV2) { r.Kind = "claim" }},
		{"installation", func(r *storageOwnerRecordV2) { r.Installation = storagebudget.ID{} }},
		{"nonce", func(r *storageOwnerRecordV2) { r.Nonce = storagebudget.ID{} }},
		{"owner", func(r *storageOwnerRecordV2) { r.HomeOwner = strings.Repeat("A", 64) }},
		{"binding-version", func(r *storageOwnerRecordV2) { r.BindingVersion = 1 }},
		{"q-binding", func(r *storageOwnerRecordV2) { r.QBinding[0] ^= 1 }},
		{"material-digest", func(r *storageOwnerRecordV2) { r.BindingMaterialDigest[0] ^= 1 }},
		{"config-directory", func(r *storageOwnerRecordV2) { r.Config.ObjectKind = 2 }},
		{"anchor-directory", func(r *storageOwnerRecordV2) { r.Anchor.ObjectKind = 2 }},
		{"bootstrap-file", func(r *storageOwnerRecordV2) { r.BootstrapLock.ObjectKind = 1 }},
		{"use-file", func(r *storageOwnerRecordV2) { r.UseLock.ObjectKind = 1 }},
		{"home-lease-file", func(r *storageOwnerRecordV2) { r.HomeLease.ObjectKind = 1 }},
		{"catalog-a-file", func(r *storageOwnerRecordV2) { r.CatalogA.ObjectKind = 1 }},
		{"catalog-b-file", func(r *storageOwnerRecordV2) { r.CatalogB.ObjectKind = 1 }},
		{"duplicate-catalog-object", func(r *storageOwnerRecordV2) { r.CatalogB = r.CatalogA }},
		{"profile-set", func(r *storageOwnerRecordV2) { r.ProfileSetDigest = storagebudget.Digest{} }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := ownerTestRecord()
			tc.change(&r)
			// Give invalid structural fields internally consistent hashes so a stale
			// binding cannot accidentally be the only reason for refusal.
			if tc.name != "q-binding" && tc.name != "material-digest" {
				var err error
				r.BindingMaterialDigest, r.QBinding, err = storageOwnerRecordDigests(r)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := storageOwnerEncodeRecordV2(r); err == nil {
				t.Fatal("malformed record encoded")
			}
			payload, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := storageOwnerDecodeRecordV2(ownerTestWire(string(payload), false)); err == nil || got != (storageOwnerRecordV2{}) {
				t.Fatal("malformed record decoded")
			}
		})
	}
	t.Run("APFS-record-and-claim", func(t *testing.T) {
		r, _ := ownerTestRecord()
		for _, p := range []*storageOwnerPersistentObject{&r.Config, &r.Anchor, &r.BootstrapLock, &r.UseLock, &r.HomeLease, &r.CatalogA, &r.CatalogB} {
			p.Backend = 2
			p.IncarnationKind = 2
			p.Incarnation = 100
		}
		var err error
		r.BindingMaterialDigest, r.QBinding, err = storageOwnerRecordDigests(r)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := storageOwnerEncodeRecordV2(r)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := storageOwnerDecodeRecordV2(raw); err != nil || got != r {
			t.Fatalf("APFS record: %v", err)
		}
		c := ownerTestClaimValue()
		c.Config = r.Config
		raw, err = storageOwnerEncodeClaimV2(c)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := storageOwnerDecodeClaimV2(raw); err != nil || got != c || !got.matches(r) {
			t.Fatalf("APFS claim: %v", err)
		}
	})
}
