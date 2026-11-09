//go:build linux && amd64

package desk

import (
	"encoding/binary"
	"errors"
	"golang.org/x/sys/unix"
	"math"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeExt4UUIDPacking(t *testing.T) {
	// Public x86-64 UAPI requests; the flexible-array header is eight bytes.
	if storageNativeExt4GetUUID != 0x8008662c || storageNativeFSGetXAttr != 0x801c581f {
		t.Fatal("unexpected ioctl ABI")
	}
	p := []byte{16, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got, err := storageNativeExt4UUIDRecord(p)
	if err != nil || got != [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16} {
		t.Fatalf("%x %v", got, err)
	}
	for _, tc := range []struct {
		offset int
		value  uint32
	}{{0, 0}, {0, 15}, {0, 17}, {4, 1}} {
		bad := append([]byte(nil), p...)
		binary.LittleEndian.PutUint32(bad[tc.offset:tc.offset+4], tc.value)
		got, err := storageNativeExt4UUIDRecord(bad)
		if got != [16]byte{} || !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("%x %v", got, err)
		}
	}
	if _, err := storageNativeExt4UUIDRecord(p[:23]); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal(err)
	}
	if _, err := storageNativeExt4UUIDRecord(append(p, 0)); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal(err)
	}
	zero := make([]byte, 24)
	zero[0] = 16
	if _, err := storageNativeExt4UUIDRecord(zero); !errors.Is(err, storagebudget.ErrUnknown) {
		t.Fatal(err)
	}
	// Invalid request/length combinations refuse before invoking the kernel.
	if err := storageNativeLinuxIOCTL(-1, 0, make([]byte, 24)); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal(err)
	}
}

func TestStorageNativeExt4ProjectPacking(t *testing.T) {
	p := []byte{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	project, flags, err := storageNativeExt4ProjectRecord(p)
	if err != nil || project != 0x11223344 || flags != 0x200 {
		t.Fatalf("project=%x flags=%x %v", project, flags, err)
	}
	if _, _, err := storageNativeExt4ProjectRecord(p[:27]); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal(err)
	}
	p[20] = 1
	if project, flags, err := storageNativeExt4ProjectRecord(p); project != 0 || flags != 0 || !errors.Is(err, storagebudget.ErrUnknown) {
		t.Fatalf("reserved bytes: %x %x %v", project, flags, err)
	}
}

func TestStorageNativeExt4GenerationPacking(t *testing.T) {
	if storageNativeFSGetVersion != 0x80087601 || uintptr(2)<<30|uintptr(8)<<16|uintptr(0x76)<<8|1 != storageNativeFSGetVersion {
		t.Fatal("generation ioctl ABI")
	}
	for _, tc := range []struct {
		p    []byte
		want uint32
	}{
		{[]byte{0, 0, 0, 0, 0, 0, 0, 0}, 0},
		{[]byte{1, 0, 0, 0, 0, 0, 0, 0}, 1},
		{[]byte{0, 0, 0, 0x80, 0, 0, 0, 0}, 0x80000000},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}, 0xffffffff},
		{[]byte{0x78, 0x56, 0x34, 0x12, 0xa5, 0xa5, 0xa5, 0xa5}, 0x12345678},
	} {
		got, err := storageNativeExt4GenerationRecord(tc.p)
		if err != nil || got != tc.want || uint64(got) != uint64(tc.want) {
			t.Fatalf("%x %v", got, err)
		}
	}
	for _, size := range []int{0, 4, 7, 9} {
		got, err := storageNativeExt4GenerationRecord(make([]byte, size))
		if got != 0 || !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("size %d: %x %v", size, got, err)
		}
	}
	for _, tc := range []struct {
		request uintptr
		size    int
	}{{0x80087601, 4}, {0x80087601, 7}, {0x40087602, 8}, {0x8008662c, 8}, {0, 8}, {0x80087601, 0}} {
		// Invalid pairs refuse without entering SYS_IOCTL, even with FD -1.
		if err := storageNativeLinuxIOCTL(-1, tc.request, make([]byte, tc.size)); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("pair %+v: %v", tc, err)
		}
	}
	if uint64(^uint(0)>>1) > math.MaxInt32 {
		over := int64(math.MaxInt32) + 1
		if err := storageNativeLinuxIOCTL(int(over), 0x80087601, make([]byte, 8)); !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatal(err)
		}
	}
}

// Handwritten responses describe ABI fields, never observations of host storage.
func storageNativeExt4ObjectFixture(t *testing.T, step int, fd int, request uintptr, p []byte) {
	t.Helper()
	if fd != 17 {
		t.Fatal("changed descriptor")
	}
	switch step {
	case 1:
		if request != 0x8008662c || len(p) != 24 || binary.LittleEndian.Uint32(p[:4]) != 16 {
			t.Fatal("UUID request")
		}
		for _, b := range p[4:] {
			if b != 0 {
				t.Fatal("UUID buffer not initialized")
			}
		}
		copy(p[8:], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	case 2:
		if request != 0x801c581f || len(p) != 28 {
			t.Fatal("attrs request")
		}
		for _, b := range p {
			if b != 0 {
				t.Fatal("attrs buffer not initialized")
			}
		}
		binary.LittleEndian.PutUint32(p[:4], 0x200)
		binary.LittleEndian.PutUint32(p[12:16], 0x11223344)
	case 3:
		if request != 0x80087601 || len(p) != 8 {
			t.Fatal("generation request")
		}
		for _, b := range p {
			if b != 0 {
				t.Fatal("generation buffer not initialized")
			}
		}
		copy(p, []byte{0xff, 0xff, 0xff, 0xff, 0xa5, 0xa5, 0xa5, 0xa5})
	default:
		t.Fatal("extra query")
	}
}

func TestStorageNativeExt4ObjectQueryOrder(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		calls := 0
		got, err := storageNativeReadExt4Object(17, kind, func(fd int, request uintptr, p []byte) error {
			calls++
			storageNativeExt4ObjectFixture(t, calls, fd, request, p)
			return nil
		})
		if err != nil || calls != 3 || got.Kind != kind || got.UUID != ([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}) || got.ProjectID != 0x11223344 || got.XFlags != 0x200 || got.InodeGeneration != 0xffffffff || !got.Started.IsZero() {
			t.Fatalf("calls=%d %+v %v", calls, got, err)
		}
	}
	calls := 0
	got, err := storageNativeReadExt4Object(17, 2, func(fd int, request uintptr, p []byte) error {
		calls++
		storageNativeExt4ObjectFixture(t, calls, fd, request, p)
		if calls > 1 {
			for i := range p {
				p[i] = 0
			}
		}
		return nil
	})
	if err != nil || calls != 3 || got.Kind != 2 || got.ProjectID != 0 || got.XFlags != 0 || got.InodeGeneration != 0 {
		t.Fatalf("raw zero %+v %v", got, err)
	}
}

func TestStorageNativeExt4ObjectPartialRefusals(t *testing.T) {
	for _, errno := range []error{unix.ENOSYS, unix.ENOTTY, unix.EOPNOTSUPP, unix.EPERM, unix.EACCES, unix.EINVAL, unix.EFAULT, unix.EIO, unix.EBADF} {
		for failAt := 1; failAt <= 3; failAt++ {
			calls := 0
			got, err := storageNativeReadExt4Object(17, 2, func(fd int, request uintptr, p []byte) error {
				calls++
				storageNativeExt4ObjectFixture(t, calls, fd, request, p)
				if calls == failAt {
					return errno
				}
				return nil
			})
			want := storagebudget.ErrUnknown
			if errno == unix.ENOSYS || errno == unix.ENOTTY || errno == unix.EOPNOTSUPP {
				want = storagebudget.ErrUnsupported
			}
			if got != (storageNativeExt4ObjectRaw{}) || calls != failAt || !errors.Is(err, want) || !errors.Is(err, errno) {
				t.Fatalf("step %d calls %d: %+v %v", failAt, calls, got, err)
			}
			if (errno == unix.EPERM || errno == unix.EACCES) && !errors.Is(err, os.ErrPermission) {
				t.Fatal("permission cause lost")
			}
		}
	}
	for _, badAt := range []int{1, 2} {
		calls := 0
		got, err := storageNativeReadExt4Object(17, 2, func(fd int, request uintptr, p []byte) error {
			calls++
			storageNativeExt4ObjectFixture(t, calls, fd, request, p)
			if calls == badAt {
				if badAt == 1 {
					p[4] = 1
				} else {
					p[20] = 1
				}
			}
			return nil
		})
		if got != (storageNativeExt4ObjectRaw{}) || err == nil || calls != badAt {
			t.Fatalf("bad parse escaped: %+v %v calls %d", got, err, calls)
		}
	}
}

func TestStorageNativeExt4ObjectInvalidInput(t *testing.T) {
	calls := 0
	query := func(int, uintptr, []byte) error { calls++; return nil }
	for _, tc := range []struct {
		fd   int
		kind uint8
	}{{-1, 1}, {17, 0}, {17, 3}} {
		got, err := storageNativeReadExt4Object(tc.fd, tc.kind, query)
		if got != (storageNativeExt4ObjectRaw{}) || !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if got, err := storageNativeReadExt4Object(17, 2, nil); got != (storageNativeExt4ObjectRaw{}) || !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("%+v %v", got, err)
	}
	if uint64(^uint(0)>>1) > math.MaxInt32 {
		over := int64(math.MaxInt32) + 1
		got, err := storageNativeReadExt4Object(int(over), 2, query)
		if got != (storageNativeExt4ObjectRaw{}) || !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input queried")
	}
}
