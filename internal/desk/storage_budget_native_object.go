package desk

import "time"

const (
	storageNativeObjectDirectory uint8 = 1
	storageNativeObjectRegular   uint8 = 2
)

// These exact-object observations confer no custody, membership or profile.
// InodeGeneration is the raw uint32 ext4 value, not durable Root.Generation.
// Random generation values do not discriminate clones or prove inode reuse.
type storageNativeExt4ObjectRaw struct {
	Started                            time.Time
	Kind                               uint8
	UUID                               [16]byte
	ProjectID, XFlags, InodeGeneration uint32
}
