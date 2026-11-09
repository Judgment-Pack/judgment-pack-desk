//go:build linux && amd64

package desk

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

// These public GET operations are classified non-writing by quotactl's mount
// write-access gate. No setter, synchronization, quota-on/off or privilege
// escalation is exposed. Kernel internals are not promised to perform no I/O.
func storageNativeQuotaStatus(ctx context.Context, f *os.File, kind storageNativeQuotaKind) (storageNativeQuotaStatusRaw, error) {
	p, metadata, err := storageNativeQuotaQuery(ctx, f, storageNativeQuotaStatusOp, storageNativeQuotaSubject{Kind: kind})
	if err != nil {
		return storageNativeQuotaStatusRaw{}, err
	}
	out, err := storageNativeQuotaStatusRecord(p, kind)
	if err != nil {
		return storageNativeQuotaStatusRaw{}, err
	}
	out.Started, out.FilesystemType, out.FSID = metadata.Started, metadata.Type, metadata.FSID
	return out, nil
}

func storageNativeQuotaInfo(ctx context.Context, f *os.File, kind storageNativeQuotaKind) (storageNativeQuotaInfoRaw, error) {
	p, metadata, err := storageNativeQuotaQuery(ctx, f, storageNativeQuotaInfoOp, storageNativeQuotaSubject{Kind: kind})
	if err != nil {
		return storageNativeQuotaInfoRaw{}, err
	}
	out, err := storageNativeQuotaInfoRecord(p, kind)
	if err != nil {
		return storageNativeQuotaInfoRaw{}, err
	}
	out.Started, out.FilesystemType, out.FSID = metadata.Started, metadata.Type, metadata.FSID
	return out, nil
}

func storageNativeQuotaSubjectQuery(ctx context.Context, f *os.File, subject storageNativeQuotaSubject) (storageNativeQuotaDiskRaw, error) {
	p, metadata, err := storageNativeQuotaQuery(ctx, f, storageNativeQuotaDiskOp, subject)
	if err != nil {
		return storageNativeQuotaDiskRaw{}, err
	}
	out, err := storageNativeQuotaDiskRecord(p, subject)
	if err != nil {
		return storageNativeQuotaDiskRaw{}, err
	}
	out.Started, out.FilesystemType, out.FSID = metadata.Started, metadata.Type, metadata.FSID
	return out, nil
}

func storageNativeQuotaQuery(ctx context.Context, f *os.File, op storageNativeQuotaOp, subject storageNativeQuotaSubject) ([]byte, storageNativeStatfsRaw, error) {
	metadata := storageNativeStatfsRaw{Started: time.Now()}
	var payload []byte
	err := storageNativeControl(ctx, f, func(fd int) error {
		if err := storageNativeDirectoryFD(fd); err != nil {
			return err
		}
		var stat unix.Statfs_t
		if err := unix.Fstatfs(fd, &stat); err != nil {
			return storageNativeLinuxError(err)
		}
		metadata.Type, metadata.FSID = uint64(stat.Type), stat.Fsid.Val
		var err error
		payload, err = storageNativeQuotaReadFD(fd, op, subject, storageNativeQuotaSyscall)
		return err
	})
	if err != nil {
		return nil, storageNativeStatfsRaw{}, err
	}
	return payload, metadata, nil
}

// An explicit callback permits handmade protocol/error fixtures, not a global
// injectable production observer. All returned payloads remain raw ABI facts.
func storageNativeQuotaReadFD(fd int, op storageNativeQuotaOp, subject storageNativeQuotaSubject, query func(int, uint32, uint32, []byte) error) ([]byte, error) {
	if fd < 0 || query == nil {
		return nil, os.ErrInvalid
	}
	command, size, err := storageNativeQuotaCommand(op, subject.Kind)
	if err != nil {
		return nil, err
	}
	if op != storageNativeQuotaDiskOp && subject.ID != 0 {
		return nil, storagebudget.ErrBounds
	}
	var buffer [storageNativeQuotaStatusBytes]byte
	p := buffer[:size]
	if op == storageNativeQuotaStatusOp {
		p[0] = 1
	} // QSTATV version request.
	if err := query(fd, command, subject.ID, p); err != nil {
		return nil, storageNativeQuotaFailure(op, err)
	}
	return p, nil
}

func storageNativeQuotaSyscall(fd int, command, id uint32, p []byte) error {
	if fd < 0 {
		return os.ErrInvalid
	}
	// Accept only the three published GET requests and their exact ABI lengths.
	kind := storageNativeQuotaKind(command & 0xff)
	valid := false
	for _, op := range []storageNativeQuotaOp{storageNativeQuotaStatusOp, storageNativeQuotaInfoOp, storageNativeQuotaDiskOp} {
		want, size, err := storageNativeQuotaCommand(op, kind)
		if err == nil && want == command && size == len(p) && (op == storageNativeQuotaDiskOp || id == 0) {
			valid = true
			break
		}
	}
	if !valid {
		return storagebudget.ErrBounds
	}
	_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL_FD, uintptr(fd), uintptr(command), uintptr(id), uintptr(unsafe.Pointer(&p[0])), 0, 0)
	runtime.KeepAlive(p)
	if errno != 0 {
		return errno
	}
	return nil
}

func storageNativeQuotaFailure(op storageNativeQuotaOp, err error) error {
	if err == nil {
		return nil
	}
	if op == storageNativeQuotaInfoOp && errors.Is(err, unix.ESRCH) {
		return errors.Join(storagebudget.ErrUnknown, errStorageNativeQuotaDisabled, err)
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EOPNOTSUPP) {
		// In particular Q_XGETQSTATV ENOSYS can mean all quotas disabled OR
		// no supported getter. It is an unavailable primitive, not a disabled
		// fact. Keep Unknown as well, prohibiting missing-constraint omission.
		return errors.Join(storagebudget.ErrUnknown, storagebudget.ErrUnsupported, err)
	}
	// EPERM/EACCES retain os.ErrPermission semantics. Subject ESRCH, EINVAL
	// (including an unmapped ID), I/O and all other errors remain unknown.
	return errors.Join(storagebudget.ErrUnknown, err)
}
