package desk

import (
	"crypto/sha256"
	"encoding/binary"
	"math"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageAdvisoryRoundBytes         uint64 = 65536
	storageAdvisoryByteMultiplier     uint64 = 2
	storageAdvisoryGroupMetadataUnits uint64 = 4
	storageAdvisoryInodeMargin        uint64 = 2
	// This is an authored policy cap, not measured native inode capacity.
	storageAdvisoryInodePolicyCap     uint64 = 1048576
	storageAdvisoryHeadroomMinBytes   uint64 = 536870912
	storageAdvisoryHeadroomMaxBytes   uint64 = 2147483648
	storageAdvisoryHeadroomDivisor    uint64 = 100
	storageAdvisoryInodeHeadroomFloor uint64 = 1024
	storageAdvisoryInitialMaxAgeNS    uint64 = 5000000000
	storageAdvisoryPassDeadlineNS     uint64 = 250000000
)

// storageAdvisoryEstimate authors conservative finite policy demand for one
// bounded persistence group. Inputs include its logical coexistence/settlement
// bytes, additional files/directories and bounded metadata-only operations.
// The estimate is not a measured or universal allocation bound. Callers retain
// the logical limits and finite operation/group bounds independently.
func storageAdvisoryEstimate(bytes, files, directories, metadataOps uint64) (storagebudget.CapacityVector, error) {
	if bytes == 0 && files == 0 && directories == 0 && metadataOps == 0 {
		return storagebudget.CapacityVector{}, nil
	}
	rounded := bytes
	if remainder := bytes % storageAdvisoryRoundBytes; remainder != 0 {
		padding := storageAdvisoryRoundBytes - remainder
		if rounded > math.MaxUint64-padding {
			return storagebudget.CapacityVector{}, storagebudget.ErrBounds
		}
		rounded += padding
	}
	if files > math.MaxUint64-directories {
		return storagebudget.CapacityVector{}, storagebudget.ErrBounds
	}
	namespace := files + directories
	if namespace > math.MaxUint64-storageAdvisoryInodeMargin || namespace > math.MaxUint64-metadataOps {
		return storagebudget.CapacityVector{}, storagebudget.ErrBounds
	}
	metadata := namespace + metadataOps
	if metadata > math.MaxUint64-storageAdvisoryGroupMetadataUnits {
		return storagebudget.CapacityVector{}, storagebudget.ErrBounds
	}
	metadata += storageAdvisoryGroupMetadataUnits
	if rounded > math.MaxUint64/storageAdvisoryByteMultiplier || metadata > math.MaxUint64/storageAdvisoryRoundBytes {
		return storagebudget.CapacityVector{}, storagebudget.ErrBounds
	}
	dataBytes := rounded * storageAdvisoryByteMultiplier
	metadataBytes := metadata * storageAdvisoryRoundBytes
	if dataBytes > math.MaxUint64-metadataBytes {
		return storagebudget.CapacityVector{}, storagebudget.ErrBounds
	}
	// Every nonempty group has policy inode demand, including metadata-only
	// groups and native models with documented dynamic inode allocation.
	return storagebudget.CapacityVector{Bytes: dataBytes + metadataBytes, Inodes: namespace + storageAdvisoryInodeMargin}, nil
}

// storageAdvisoryPolicyMaterial returns fresh version-2 policy bytes. Reopen
// must preserve the original policy and outstanding demands; this material is
// not an automatic policy migration or evidence of native qualification.
func storageAdvisoryPolicyMaterial() []byte {
	material := []byte("desk-advisory-allocation-v2\x00")
	for _, value := range [...]uint64{
		storageAdvisoryRoundBytes, storageAdvisoryByteMultiplier,
		storageAdvisoryGroupMetadataUnits, storageAdvisoryInodeMargin,
		storageAdvisoryInodePolicyCap, storageAdvisoryHeadroomMinBytes,
		storageAdvisoryHeadroomMaxBytes, storageAdvisoryHeadroomDivisor,
		storageAdvisoryInodeHeadroomFloor, storageAdvisoryInitialMaxAgeNS,
		storageAdvisoryPassDeadlineNS,
	} {
		material = binary.LittleEndian.AppendUint64(material, value)
	}
	return material
}

func storageAdvisoryPolicyEvidence() storagebudget.Digest {
	return storagebudget.Digest(sha256.Sum256(storageAdvisoryPolicyMaterial()))
}
