package desk

import (
	"errors"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageNativeQuotaKind uint8

const (
	storageNativeQuotaUser storageNativeQuotaKind = iota
	storageNativeQuotaGroup
	storageNativeQuotaProject
)

// Subject is explicit raw input, not original custody, credentials or proof of
// which UID/GID/project a prospective child will be charged to. ID is interpreted
// by the kernel in the calling process's user namespace.
type storageNativeQuotaSubject struct {
	Kind storageNativeQuotaKind
	ID   uint32
}

// Only Q_GETINFO/ESRCH gets this classification. An ESRCH from a subject query,
// or ENOSYS from the status getter, does not establish disabled accounting.
var errStorageNativeQuotaDisabled = errors.New("storage quota accounting disabled for requested type")

const (
	storageNativeQuotaStateDisabled         uint8 = 1
	storageNativeQuotaStateAccounting       uint8 = 2
	storageNativeQuotaStateEnforced         uint8 = 3
	storageNativeQuotaFinite                uint8 = 1
	storageNativeQuotaNoFiniteLimitReported uint8 = 2
)

type storageNativeQuotaFileRaw struct {
	Inode, Blocks512 uint64
	Extents          uint32
}

type storageNativeQuotaStatusRaw struct {
	Started        time.Time
	FilesystemType uint64
	FSID           [2]int32
	RequestedKind  storageNativeQuotaKind
	Version        uint8
	Flags          uint16
	InCore         uint32
	Files          [3]storageNativeQuotaFileRaw // user, group, project; independent rows
	GraceSeconds   [3]int32                     // block, inode, realtime; for RequestedKind
	WarningLimits  [3]uint16
}

// Q_GETINFO carries a true validity mask, but does not expose enforcement state.
// Success establishes returned information, not that limits are enforced.
type storageNativeQuotaInfoRaw struct {
	Started                              time.Time
	FilesystemType                       uint64
	FSID                                 [2]int32
	RequestedKind                        storageNativeQuotaKind
	BlockGraceSeconds, InodeGraceSeconds uint64
	Flags, Valid                         uint32
}

// XGETQUOTA's FieldMask is NOT a returned-value validity bitmap. Its getter
// normally returns zero, or BIGTIME for 40-bit timestamps. Units are retained.
type storageNativeQuotaDiskRaw struct {
	Started                                                    time.Time
	FilesystemType                                             uint64
	FSID                                                       [2]int32
	Subject                                                    storageNativeQuotaSubject
	Version, Flags                                             uint8
	FieldMask                                                  uint16
	BlockUnit                                                  uint16
	HardBlocks, SoftBlocks, HardInodes, SoftInodes             uint64
	UsedBlocks, UsedInodes                                     uint64
	InodeExpiry, BlockExpiry, RealtimeExpiry                   int64
	InodeWarnings, BlockWarnings, RealtimeWarnings             uint16
	RealtimeHardBlocks, RealtimeSoftBlocks, RealtimeUsedBlocks uint64
}

type storageNativeQuotaResource struct {
	State                             uint8
	LimitLower, UsageUpper, Available uint64
}

type storageNativeQuotaRemaining struct {
	Bytes, Inodes storageNativeQuotaResource
}

func storageNativeQuotaTypeFlag(kind storageNativeQuotaKind) (uint8, error) {
	switch kind {
	case storageNativeQuotaUser:
		return 1, nil
	case storageNativeQuotaGroup:
		return 4, nil // FS_GROUP_QUOTA differs from the QCMD type index.
	case storageNativeQuotaProject:
		return 2, nil
	default:
		return 0, storagebudget.ErrBounds
	}
}

func (s storageNativeQuotaStatusRaw) state() (uint8, error) {
	if _, err := storageNativeQuotaTypeFlag(s.RequestedKind); err != nil {
		return 0, err
	}
	if s.Version != 1 || s.Flags == 0 {
		return 0, storagebudget.ErrUnknown
	}
	if s.Flags&^uint16(0x3f) != 0 {
		return 0, storagebudget.ErrUnsupported
	}
	for i := uint(0); i < 3; i++ {
		pair := (s.Flags >> (2 * i)) & 3
		if pair == 2 { // enforcement without accounting is inconsistent.
			return 0, storagebudget.ErrUnknown
		}
	}
	pair := (s.Flags >> (2 * uint(s.RequestedKind))) & 3
	switch pair {
	case 0:
		return storageNativeQuotaStateDisabled, nil
	case 1:
		return storageNativeQuotaStateAccounting, nil
	default:
		return storageNativeQuotaStateEnforced, nil
	}
}

// Source-backed ext4/VFS arithmetic only. The future owner must independently
// qualify the kernel/backend, status/enforcement, exact descriptor and complete
// subject membership. This produces no CapacityObservation or missing-constraint
// permission. Other backends' pending accounting is deliberately not inferred.
func storageNativeQuotaExt4Remaining(raw storageNativeQuotaDiskRaw) (storageNativeQuotaRemaining, error) {
	var out storageNativeQuotaRemaining
	flag, err := storageNativeQuotaTypeFlag(raw.Subject.Kind)
	if err != nil {
		return out, err
	}
	if raw.FilesystemType != 0xef53 || raw.Version != 1 || raw.Flags != flag || raw.BlockUnit != 512 || raw.FieldMask&^uint16(0x8000) != 0 {
		return out, storagebudget.ErrUnsupported
	}
	if raw.RealtimeHardBlocks != 0 || raw.RealtimeSoftBlocks != 0 || raw.RealtimeUsedBlocks != 0 || raw.RealtimeExpiry != 0 || raw.RealtimeWarnings != 0 {
		return out, storagebudget.ErrUnsupported
	}
	// VFS dquot_get_dqblk includes curspace+rsvspace; XGETQUOTA rounds all
	// byte values upward to 512-byte blocks. Use an upper bound for usage and
	// a LOWER bound for a positive limit, never the rounded-up limit as a grant.
	for _, n := range []uint64{raw.HardBlocks, raw.SoftBlocks, raw.UsedBlocks} {
		if n > (1<<60)/512 {
			return out, storagebudget.ErrBounds
		}
	}
	for _, n := range []uint64{raw.HardInodes, raw.SoftInodes, raw.UsedInodes} {
		if n > 1<<40 {
			return out, storagebudget.ErrBounds
		}
	}
	out.Bytes = storageNativeQuotaFiniteResource(raw.HardBlocks, raw.SoftBlocks, raw.UsedBlocks, 512)
	out.Inodes = storageNativeQuotaFiniteResource(raw.HardInodes, raw.SoftInodes, raw.UsedInodes, 1)
	return out, nil
}

// Bounds and a verified unit/rounding model must precede this helper. Using the
// lower nonzero soft/hard cap is conservative irrespective of grace timers.
func storageNativeQuotaFiniteResource(hard, soft, used, unit uint64) storageNativeQuotaResource {
	limit := hard
	if limit == 0 || soft != 0 && soft < limit {
		limit = soft
	}
	out := storageNativeQuotaResource{State: storageNativeQuotaNoFiniteLimitReported, UsageUpper: used * unit}
	if limit == 0 {
		// No numeric infinity, known available count or authority to omit a
		// restriction follows from the raw zero values.
		return out
	}
	out.State = storageNativeQuotaFinite
	out.LimitLower = (limit-1)*unit + 1
	if out.UsageUpper < out.LimitLower {
		out.Available = out.LimitLower - out.UsageUpper
	}
	return out
}
