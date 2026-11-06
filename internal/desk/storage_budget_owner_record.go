package desk

// Detached v2 wire data only. These codecs cannot open a path, mint custody,
// qualify a platform profile, or activate an installation.
import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageOwnerHeaderBytes = 64
	storageOwnerRecordBytes = 4096
)

var (
	storageOwnerRecordMagic = [8]byte{'D', 'S', 'K', 'I', 'N', 'S', '2', 0}
	storageOwnerClaimMagic  = [8]byte{'D', 'S', 'K', 'C', 'L', 'M', '2', 0}
)

type storageOwnerPersistentObject struct {
	Backend         uint8
	VolumeUUID      [16]byte
	Object          uint64
	Incarnation     uint64
	IncarnationKind uint8
	ObjectKind      uint8
}

func (p storageOwnerPersistentObject) valid(kind uint8) bool {
	if p.VolumeUUID == [16]byte{} || p.Object == 0 || p.ObjectKind != kind || kind < 1 || kind > 2 {
		return false
	}
	switch p.Backend {
	case 1:
		return p.IncarnationKind == 1 && p.Incarnation <= math.MaxUint32
	case 2:
		return p.IncarnationKind == 2 && p.Incarnation != 0
	}
	return false
}

type storageOwnerRecordV2 struct {
	Version               uint32
	Kind                  string
	Installation          storagebudget.ID
	Nonce                 storagebudget.ID
	HomeOwner             string
	BindingVersion        uint8
	QBinding              storagebudget.Digest
	BindingMaterialDigest storagebudget.Digest
	Config                storageOwnerPersistentObject
	Anchor                storageOwnerPersistentObject
	BootstrapLock         storageOwnerPersistentObject
	UseLock               storageOwnerPersistentObject
	HomeLease             storageOwnerPersistentObject
	CatalogA              storageOwnerPersistentObject
	CatalogB              storageOwnerPersistentObject
	// This digest is a binding reference, not proof of an installed supported profile.
	ProfileSetDigest storagebudget.Digest
}

// Explicit material type makes the self-hash exclusion a closed wire contract.
type storageOwnerBindingMaterialV2 struct {
	Version          uint32
	Kind             string
	Installation     storagebudget.ID
	Nonce            storagebudget.ID
	HomeOwner        string
	BindingVersion   uint8
	Config           storageOwnerPersistentObject
	Anchor           storageOwnerPersistentObject
	BootstrapLock    storageOwnerPersistentObject
	UseLock          storageOwnerPersistentObject
	HomeLease        storageOwnerPersistentObject
	CatalogA         storageOwnerPersistentObject
	CatalogB         storageOwnerPersistentObject
	ProfileSetDigest storagebudget.Digest
}

func (r storageOwnerRecordV2) bindingMaterial() storageOwnerBindingMaterialV2 {
	return storageOwnerBindingMaterialV2{r.Version, r.Kind, r.Installation, r.Nonce, r.HomeOwner, r.BindingVersion, r.Config, r.Anchor, r.BootstrapLock, r.UseLock, r.HomeLease, r.CatalogA, r.CatalogB, r.ProfileSetDigest}
}

// This computes comparison data, never a replacement binding on decode.
func storageOwnerRecordDigests(r storageOwnerRecordV2) (storagebudget.Digest, storagebudget.Digest, error) {
	if len(r.Kind) > 12 || len(r.HomeOwner) > 64 {
		return storagebudget.Digest{}, storagebudget.Digest{}, storagebudget.ErrBounds
	}
	b, err := json.Marshal(r.bindingMaterial())
	if err != nil {
		return storagebudget.Digest{}, storagebudget.Digest{}, err
	}
	material := sha256.Sum256(b)
	h := sha256.New()
	h.Write([]byte("desk-storage-installation-v2\x00"))
	h.Write(b)
	var binding storagebudget.Digest
	copy(binding[:], h.Sum(nil))
	return material, binding, nil
}

func storageOwnerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := range s {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

func (r storageOwnerRecordV2) validate() error {
	if r.Version != 2 || r.Kind != "installation" || r.BindingVersion != 2 || r.Installation == (storagebudget.ID{}) || r.Nonce == (storagebudget.ID{}) || !storageOwnerHex(r.HomeOwner, 64) || r.ProfileSetDigest == (storagebudget.Digest{}) || r.QBinding == (storagebudget.Digest{}) || r.BindingMaterialDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	if !r.Config.valid(1) || !r.Anchor.valid(1) || !r.BootstrapLock.valid(2) || !r.UseLock.valid(2) || !r.HomeLease.valid(2) || !r.CatalogA.valid(2) || !r.CatalogB.valid(2) {
		return storagebudget.ErrBounds
	}
	// Each fixed name denotes its own object; cloning one witness is not a map.
	objects := []storageOwnerPersistentObject{r.Config, r.Anchor, r.BootstrapLock, r.UseLock, r.HomeLease, r.CatalogA, r.CatalogB}
	for i, p := range objects {
		for _, q := range objects[:i] {
			if p == q {
				return storagebudget.ErrBounds
			}
		}
	}
	m, q, err := storageOwnerRecordDigests(r)
	if err != nil || m != r.BindingMaterialDigest || q != r.QBinding {
		return storagebudget.ErrBounds
	}
	return nil
}

type storageOwnerClaimV2 struct {
	Version          uint32
	Kind             string
	Installation     storagebudget.ID
	Nonce            storagebudget.ID
	HomeOwner        string
	Config           storageOwnerPersistentObject
	ProfileSetDigest storagebudget.Digest
}

func (c storageOwnerClaimV2) validate() error {
	if c.Version != 2 || c.Kind != "claim" || c.Installation == (storagebudget.ID{}) || c.Nonce == (storagebudget.ID{}) || !storageOwnerHex(c.HomeOwner, 64) || !c.Config.valid(1) || c.ProfileSetDigest == (storagebudget.Digest{}) {
		return storagebudget.ErrBounds
	}
	return nil
}
func (c storageOwnerClaimV2) matches(r storageOwnerRecordV2) bool {
	return c.validate() == nil && r.validate() == nil && c.Installation == r.Installation && c.Nonce == r.Nonce && c.HomeOwner == r.HomeOwner && c.Config == r.Config && c.ProfileSetDigest == r.ProfileSetDigest
}

func storageOwnerEncodeFrame(magic [8]byte, size int, sequence uint64, payload []byte) ([]byte, error) {
	if sequence == 0 || size != storageOwnerRecordBytes || len(payload) > size-storageOwnerHeaderBytes {
		return nil, storagebudget.ErrBounds
	}
	out := make([]byte, size)
	copy(out[:8], magic[:])
	binary.LittleEndian.PutUint32(out[8:12], 2)
	binary.LittleEndian.PutUint32(out[12:16], storageOwnerHeaderBytes)
	binary.LittleEndian.PutUint64(out[16:24], sequence)
	binary.LittleEndian.PutUint32(out[24:28], uint32(len(payload)))
	copy(out[64:], payload)
	h := sha256.New()
	h.Write(out[:32])
	h.Write(payload)
	copy(out[32:64], h.Sum(nil))
	return out, nil
}
func storageOwnerDecodeFrame(raw []byte, magic [8]byte, size int) ([]byte, uint64, error) {
	if len(raw) != size || size != storageOwnerRecordBytes || !bytes.Equal(raw[:8], magic[:]) || binary.LittleEndian.Uint32(raw[8:12]) != 2 || binary.LittleEndian.Uint32(raw[12:16]) != 64 || binary.LittleEndian.Uint32(raw[28:32]) != 0 {
		return nil, 0, storagebudget.ErrBounds
	}
	seq := binary.LittleEndian.Uint64(raw[16:24])
	n := binary.LittleEndian.Uint32(raw[24:28])
	if seq == 0 || uint64(n) > uint64(size-64) {
		return nil, 0, storagebudget.ErrBounds
	}
	end := 64 + int(n)
	for _, b := range raw[end:] {
		if b != 0 {
			return nil, 0, storagebudget.ErrBounds
		}
	}
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64:end])
	if !bytes.Equal(raw[32:64], h.Sum(nil)) {
		return nil, 0, storagebudget.ErrBounds
	}
	return raw[64:end], seq, nil
}
func storageOwnerEncodeRecordV2(r storageOwnerRecordV2) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return storageOwnerEncodeFrame(storageOwnerRecordMagic, storageOwnerRecordBytes, 1, b)
}
func storageOwnerDecodeRecordV2(raw []byte) (storageOwnerRecordV2, error) {
	var r storageOwnerRecordV2
	b, seq, err := storageOwnerDecodeFrame(raw, storageOwnerRecordMagic, storageOwnerRecordBytes)
	if err != nil || seq != 1 {
		return r, storagebudget.ErrBounds
	}
	if err = storageOwnerClosedJSON(b, &r); err != nil {
		return storageOwnerRecordV2{}, err
	}
	if err = r.validate(); err != nil {
		return storageOwnerRecordV2{}, err
	}
	return r, nil
}
func storageOwnerEncodeClaimV2(c storageOwnerClaimV2) ([]byte, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return storageOwnerEncodeFrame(storageOwnerClaimMagic, storageOwnerRecordBytes, 1, b)
}
func storageOwnerDecodeClaimV2(raw []byte) (storageOwnerClaimV2, error) {
	var c storageOwnerClaimV2
	b, seq, err := storageOwnerDecodeFrame(raw, storageOwnerClaimMagic, storageOwnerRecordBytes)
	if err != nil || seq != 1 {
		return c, storagebudget.ErrBounds
	}
	if err = storageOwnerClosedJSON(b, &c); err != nil {
		return storageOwnerClaimV2{}, err
	}
	if err = c.validate(); err != nil {
		return storageOwnerClaimV2{}, err
	}
	return c, nil
}
