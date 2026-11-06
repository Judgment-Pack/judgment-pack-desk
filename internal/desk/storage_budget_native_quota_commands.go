package desk

import "github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"

type storageNativeQuotaOp uint8

const (
	storageNativeQuotaStatusOp storageNativeQuotaOp = 1
	storageNativeQuotaInfoOp   storageNativeQuotaOp = 2
	storageNativeQuotaDiskOp   storageNativeQuotaOp = 3
)

func storageNativeQuotaCommand(op storageNativeQuotaOp, kind storageNativeQuotaKind) (uint32, int, error) {
	if _, err := storageNativeQuotaTypeFlag(kind); err != nil {
		return 0, 0, err
	}
	var subcommand uint32
	var size int
	switch op {
	case storageNativeQuotaStatusOp:
		subcommand, size = 0x5808, storageNativeQuotaStatusBytes // XQM_CMD(8)
	case storageNativeQuotaInfoOp:
		subcommand, size = 0x800005, storageNativeQuotaInfoBytes // Q_GETINFO
	case storageNativeQuotaDiskOp:
		subcommand, size = 0x5803, storageNativeQuotaDiskBytes // XQM_CMD(3)
	default:
		return 0, 0, storagebudget.ErrBounds
	}
	// Public QCMD(subcommand, type): shift eight, then type in the low byte.
	return subcommand<<8 | uint32(kind), size, nil
}
