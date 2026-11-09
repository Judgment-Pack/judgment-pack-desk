//go:build linux

package desk

import (
	"errors"
	"math"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

func storageNativeStatxFixture(mountMask uint32) unix.Statx_t {
	return unix.Statx_t{Mask: 0x11b | mountMask, Mode: 0x41c0, Uid: 101, Gid: 102, Ino: 103, Dev_major: 8, Dev_minor: 2, Mnt_id: 104}
}

func TestStorageNativeLinuxMountSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstMask  uint32
		firstError error
		secondMask uint32
		wantKind   uint8
		wantCalls  int
		err        error
	}{
		{"unique", 0x4000, nil, 0, storageNativeMountUnique, 1, nil},
		{"ignored-unique", 0, nil, 0x1000, storageNativeMountOrdinary, 2, nil},
		{"rejected-unique", 0, unix.EINVAL, 0x1000, storageNativeMountOrdinary, 2, nil},
		{"ordinary-returned-by-primary", 0x1000, nil, 0x1000, storageNativeMountOrdinary, 2, nil},
		{"no-supported-mount-mask", 0, nil, 0, 0, 2, storagebudget.ErrUnsupported},
		{"denied", 0, unix.EACCES, 0x1000, 0, 1, os.ErrPermission},
		{"not-implemented", 0, unix.ENOSYS, 0x1000, 0, 1, storagebudget.ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := storageNativeReadLinuxObject(17, func(fd, mask int, stat *unix.Statx_t) error {
				calls++
				if fd != 17 {
					t.Fatal("descriptor changed")
				}
				if calls == 1 {
					if mask != 0x411b {
						t.Fatalf("primary request=%x", mask)
					}
					*stat = storageNativeStatxFixture(tc.firstMask)
					return tc.firstError
				}
				if calls != 2 || mask != 0x111b {
					t.Fatalf("fallback %d mask=%x", calls, mask)
				}
				// The production leaf must clear the rejected/partial response.
				if *stat != (unix.Statx_t{}) {
					t.Fatal("fallback reused prior fields")
				}
				*stat = storageNativeStatxFixture(tc.secondMask)
				return nil
			})
			if calls != tc.wantCalls || !errors.Is(err, tc.err) {
				t.Fatalf("calls=%d err=%v; want %d %v", calls, err, tc.wantCalls, tc.err)
			}
			if tc.err != nil {
				if got != (storageNativeLinuxObjectRaw{}) {
					t.Fatalf("partial result escaped: %+v", got)
				}
			} else if got.MountKind != tc.wantKind || got.MountID != 104 || got.Inode != 103 || got.UID != 101 || got.GID != 102 || got.DeviceMinor != 2 {
				t.Fatalf("facts=%+v", got)
			}
		})
	}
}

func TestStorageNativeLinuxReturnedFieldsRequired(t *testing.T) {
	for _, missing := range []uint32{1, 2, 8, 16, 256} {
		got, err := storageNativeReadLinuxObject(17, func(_ int, _ int, stat *unix.Statx_t) error {
			*stat = storageNativeStatxFixture(0x4000)
			stat.Mask &^= missing
			return nil
		})
		if got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("missing=%x %+v %v", missing, got, err)
		}
	}
	for _, badMode := range []uint16{0, 0x81c0} {
		_, err := storageNativeReadLinuxObject(17, func(_ int, _ int, stat *unix.Statx_t) error {
			*stat = storageNativeStatxFixture(0x4000)
			stat.Mode = badMode
			return nil
		})
		if !errors.Is(err, storagebudget.ErrUnknown) {
			t.Fatal(err)
		}
	}
	_, err := storageNativeReadLinuxObject(17, func(_ int, _ int, stat *unix.Statx_t) error {
		*stat = storageNativeStatxFixture(0x4000)
		stat.Mnt_id = 0
		return nil
	})
	if !errors.Is(err, storagebudget.ErrUnknown) {
		t.Fatal(err)
	}
	if _, err := storageNativeReadLinuxObject(17, nil); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestStorageNativeLinuxQueryErrorClassification(t *testing.T) {
	for _, errno := range []error{unix.ENOSYS, unix.ENOTTY, unix.EOPNOTSUPP} {
		if err := storageNativeLinuxError(errno); !errors.Is(err, storagebudget.ErrUnsupported) || !errors.Is(err, errno) {
			t.Fatal(err)
		}
	}
	for _, errno := range []error{unix.EACCES, unix.EPERM} {
		if err := storageNativeLinuxError(errno); !errors.Is(err, storagebudget.ErrUnknown) || !errors.Is(err, os.ErrPermission) {
			t.Fatal(err)
		}
	}
	if err := storageNativeLinuxError(unix.EIO); !errors.Is(err, storagebudget.ErrUnknown) || !errors.Is(err, unix.EIO) {
		t.Fatal(err)
	}
}

func TestStorageNativeLinuxObjectKind(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      uint8
		mode      uint16
		firstMask uint32
		firstErr  error
		wantKind  uint8
		wantCalls int
		wantErr   error
	}{
		{"regular-unique", 2, 0x8180, 0x4000, nil, storageNativeMountUnique, 1, nil},
		{"directory-ordinary", 1, 0x41c0, 0, unix.EINVAL, storageNativeMountOrdinary, 2, nil},
		{"regular-ordinary", 2, 0x8180, 0, nil, storageNativeMountOrdinary, 2, nil},
		{"regular-returned-directory", 2, 0x41c0, 0x4000, nil, 0, 1, storagebudget.ErrUnknown},
		{"directory-returned-regular", 1, 0x8180, 0x4000, nil, 0, 1, storagebudget.ErrUnknown},
		{"denied-no-retry", 2, 0x8180, 0, unix.EPERM, 0, 1, os.ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := storageNativeReadLinuxObjectKind(17, tc.kind, func(fd, mask int, stat *unix.Statx_t) error {
				calls++
				if fd != 17 {
					t.Fatal("changed descriptor")
				}
				if *stat != (unix.Statx_t{}) {
					t.Fatal("response not cleared")
				}
				*stat = storageNativeStatxFixture(tc.firstMask)
				stat.Mode = tc.mode
				if calls == 1 {
					if mask != 0x411b {
						t.Fatalf("primary mask %x", mask)
					}
					return tc.firstErr
				}
				if calls != 2 || mask != 0x111b {
					t.Fatal("unexpected fallback")
				}
				stat.Mask = 0x111b
				return nil
			})
			if calls != tc.wantCalls || !errors.Is(err, tc.wantErr) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if tc.wantErr != nil {
				if got != (storageNativeLinuxObjectRaw{}) {
					t.Fatalf("partial result %+v", got)
				}
				return
			}
			if got.Mode != tc.mode || got.MountKind != tc.wantKind || got.MountID != 104 || got.Inode != 103 || got.UID != 101 || got.GID != 102 {
				t.Fatalf("facts %+v", got)
			}
		})
	}
	for _, missing := range []uint32{1, 2, 8, 16, 256} {
		got, err := storageNativeReadLinuxObjectKind(17, 2, func(_, _ int, stat *unix.Statx_t) error {
			*stat = storageNativeStatxFixture(0x4000)
			stat.Mode = 0x8180
			stat.Mask &^= missing
			return nil
		})
		if got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("missing %x: %+v %v", missing, got, err)
		}
	}
}

func TestStorageNativeLinuxObjectInvalidInput(t *testing.T) {
	calls := 0
	query := func(int, int, *unix.Statx_t) error { calls++; return nil }
	for _, tc := range []struct {
		fd   int
		kind uint8
		want error
	}{{-1, 1, os.ErrInvalid}, {17, 0, os.ErrInvalid}, {17, 3, os.ErrInvalid}} {
		got, err := storageNativeReadLinuxObjectKind(tc.fd, tc.kind, query)
		if got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, tc.want) {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if got, err := storageNativeReadLinuxObjectKind(17, 2, nil); got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("%+v %v", got, err)
	}
	if uint64(^uint(0)>>1) > math.MaxInt32 {
		over := int64(math.MaxInt32) + 1
		got, err := storageNativeReadLinuxObjectKind(int(over), 2, query)
		if got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input queried")
	}
}
