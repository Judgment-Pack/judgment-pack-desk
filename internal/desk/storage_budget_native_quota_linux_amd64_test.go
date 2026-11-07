//go:build linux && amd64

package desk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

func TestStorageNativeQuotaQueryProtocol(t *testing.T) {
	if unix.SYS_QUOTACTL_FD != 443 {
		t.Fatal("unexpected pinned amd64 syscall declaration")
	}
	for _, tc := range []struct {
		op   storageNativeQuotaOp
		base uint32
		size int
	}{{storageNativeQuotaStatusOp, 0x580800, 160}, {storageNativeQuotaInfoOp, 0x80000500, 24}, {storageNativeQuotaDiskOp, 0x580300, 112}} {
		for kind := storageNativeQuotaKind(0); kind < 3; kind++ {
			subject := storageNativeQuotaSubject{Kind: kind}
			want := storageNativeQuotaStatusFixture()
			switch tc.op {
			case storageNativeQuotaInfoOp:
				want = storageNativeQuotaInfoFixture()
			case storageNativeQuotaDiskOp:
				subject.ID = 42
				want = storageNativeQuotaDiskFixture()
				want[1] = []byte{1, 4, 2}[kind]
				binary.LittleEndian.PutUint32(want[4:8], 42)
			}
			calls := 0
			got, err := storageNativeQuotaReadFD(17, tc.op, subject, func(fd int, command, id uint32, p []byte) error {
				calls++
				if fd != 17 || command != tc.base+uint32(kind) || id != subject.ID || len(p) != tc.size {
					t.Fatalf("fd=%d cmd=%x id=%d len=%d", fd, command, id, len(p))
				}
				initial := make([]byte, tc.size)
				if tc.op == storageNativeQuotaStatusOp {
					initial[0] = 1
				}
				if !bytes.Equal(p, initial) {
					t.Fatalf("wrong input buffer: %x", p)
				}
				copy(p, want)
				return nil
			})
			if err != nil || calls != 1 || !bytes.Equal(got, want) {
				t.Fatalf("op=%d kind=%d calls=%d err=%v", tc.op, kind, calls, err)
			}
		}
	}
}

func TestStorageNativeQuotaErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		op                                storageNativeQuotaOp
		cause                             error
		disabled, unsupported, permission bool
	}{
		{"status-enosys-ambiguous", storageNativeQuotaStatusOp, unix.ENOSYS, false, true, false},
		{"info-esrch-disabled", storageNativeQuotaInfoOp, unix.ESRCH, true, false, false},
		{"subject-esrch-unknown", storageNativeQuotaDiskOp, unix.ESRCH, false, false, false},
		{"status-esrch-unknown", storageNativeQuotaStatusOp, unix.ESRCH, false, false, false},
		{"subject-denied", storageNativeQuotaDiskOp, unix.EPERM, false, false, true},
		{"info-denied", storageNativeQuotaInfoOp, unix.EACCES, false, false, true},
		{"unsupported-command", storageNativeQuotaDiskOp, unix.EOPNOTSUPP, false, true, false},
		{"unmapped-subject", storageNativeQuotaDiskOp, unix.EINVAL, false, false, false},
		{"io", storageNativeQuotaStatusOp, unix.EIO, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject := storageNativeQuotaSubject{0, 0}
			if tc.op == storageNativeQuotaDiskOp {
				subject.ID = 42
			}
			got, err := storageNativeQuotaReadFD(17, tc.op, subject, func(_ int, _ uint32, _ uint32, p []byte) error { p[0] = 99; return tc.cause })
			if got != nil || !errors.Is(err, tc.cause) || !errors.Is(err, storagebudget.ErrUnknown) || errors.Is(err, errStorageNativeQuotaDisabled) != tc.disabled || errors.Is(err, storagebudget.ErrUnsupported) != tc.unsupported || errors.Is(err, os.ErrPermission) != tc.permission {
				t.Fatalf("payload=%x err=%v", got, err)
			}
		})
	}
}

func TestStorageNativeQuotaQueryGuards(t *testing.T) {
	called := false
	query := func(int, uint32, uint32, []byte) error { called = true; return nil }
	for _, tc := range []struct {
		fd      int
		op      storageNativeQuotaOp
		subject storageNativeQuotaSubject
		want    error
	}{
		{-1, storageNativeQuotaStatusOp, storageNativeQuotaSubject{}, os.ErrInvalid},
		{17, 0, storageNativeQuotaSubject{}, storagebudget.ErrBounds},
		{17, storageNativeQuotaDiskOp, storageNativeQuotaSubject{3, 42}, storagebudget.ErrBounds},
		{17, storageNativeQuotaInfoOp, storageNativeQuotaSubject{0, 42}, storagebudget.ErrBounds},
	} {
		if p, err := storageNativeQuotaReadFD(tc.fd, tc.op, tc.subject, query); p != nil || !errors.Is(err, tc.want) {
			t.Fatalf("%x %v", p, err)
		}
	}
	if _, err := storageNativeQuotaReadFD(17, storageNativeQuotaStatusOp, storageNativeQuotaSubject{}, nil); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
	if called {
		t.Fatal("invalid query reached callback")
	}
	// Invalid fixed request/length combinations refuse before the syscall.
	for _, tc := range []struct {
		command, id uint32
		size        int
	}{{0, 0, 160}, {0x580800, 0, 159}, {0x580803, 0, 160}, {0x80000500, 1, 24}, {0x580300, 42, 111}, {0x580400, 42, 112}} {
		if err := storageNativeQuotaSyscall(17, tc.command, tc.id, make([]byte, tc.size)); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("%x %d %d: %v", tc.command, tc.id, tc.size, err)
		}
	}
	if err := storageNativeQuotaSyscall(-1, 0x580800, 0, make([]byte, 160)); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
}
