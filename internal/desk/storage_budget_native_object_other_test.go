//go:build !linux || !amd64

package desk

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeExt4ObjectUnavailable(t *testing.T) {
	got, err := storageNativeExt4Object(context.Background(), nil)
	if got != (storageNativeExt4ObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStorageNativeLinuxObjectMetadataUnavailable(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux implementation is available")
	}
	got, err := storageNativeLinuxObjectMetadata(context.Background(), nil)
	if got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %v", got, err)
	}
}
