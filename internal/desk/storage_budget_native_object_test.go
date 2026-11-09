//go:build linux || darwin

package desk

import (
	"context"
	"errors"
	"math"
	"os"
	"runtime"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeNormalObjectClassifier(t *testing.T) {
	for _, tc := range []struct {
		mode uint32
		kind uint8
		want error
	}{
		{0x41c0, 1, nil}, {0x8180, 2, nil}, {0, 0, os.ErrInvalid}, {0xa000, 0, os.ErrInvalid}, {0x1000, 0, os.ErrInvalid}, {0xc000, 0, os.ErrInvalid}, {0x2000, 0, os.ErrInvalid}, {0x6000, 0, os.ErrInvalid},
	} {
		kind, err := storageNativeObjectKind(0, tc.mode)
		if kind != tc.kind || !errors.Is(err, tc.want) {
			t.Fatalf("mode %x: %d %v", tc.mode, kind, err)
		}
	}
	// Handwritten public flag values, kept independent of platform implementation.
	forbidden := 0x200000 // Linux O_PATH
	if runtime.GOOS == "darwin" {
		forbidden = 0x8000
	} // O_EVTONLY
	for _, mode := range []uint32{0x41c0, 0x8180} {
		kind, err := storageNativeObjectKind(forbidden, mode)
		if kind != 0 || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("special flag: %d %v", kind, err)
		}
	}
	if kind, err := storageNativeObjectFD(-1); kind != 0 || !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("negative: %d %v", kind, err)
	}
	if uint64(^uint(0)>>1) > math.MaxInt32 {
		over := int64(math.MaxInt32) + 1
		if kind, err := storageNativeObjectFD(int(over)); kind != 0 || !errors.Is(err, storagebudget.ErrBounds) {
			t.Fatalf("oversized: %d %v", kind, err)
		}
	}
}

func TestStorageNativeNormalObjectBorrow(t *testing.T) {
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	file, err := os.CreateTemp(t.TempDir(), "object")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, tc := range []struct {
		f    *os.File
		kind uint8
	}{{dir, 1}, {file, 2}} {
		original := tc.f.Fd()
		if err := storageNativeControl(context.Background(), tc.f, func(fd int) error {
			if uintptr(fd) != original {
				t.Fatal("borrow changed descriptor")
			}
			kind, err := storageNativeObjectFD(fd)
			if kind != tc.kind || err != nil {
				t.Fatalf("kind %d %v", kind, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tc.f.Stat(); err != nil {
			t.Fatalf("borrow closed input %v", err)
		}
	}
	if err := storageNativeControl(context.Background(), file, storageNativeDirectoryFD); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("old directory guard widened: %v", err)
	}
}

func TestStorageNativeObjectEntryCancelled(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "held")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// No native query can run: Control checks the cancelled context first.
	if runtime.GOOS == "linux" {
		got, err := storageNativeLinuxObjectMetadata(ctx, file)
		if got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, context.Canceled) {
			t.Fatalf("%+v %v", got, err)
		}
		if runtime.GOARCH == "amd64" {
			got, err := storageNativeExt4Object(ctx, file)
			if got != (storageNativeExt4ObjectRaw{}) || !errors.Is(err, context.Canceled) {
				t.Fatalf("%+v %v", got, err)
			}
		}
	}
	// Darwin cgo availability has a separate build-tagged refusal fixture.
	if _, err := file.Stat(); err != nil {
		t.Fatalf("cancel closed original %v", err)
	}
}
