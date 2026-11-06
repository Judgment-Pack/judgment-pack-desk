//go:build !darwin || !cgo || (!amd64 && !arm64)

package desk

import (
	"context"
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeDarwinBridgeUnavailable(t *testing.T) {
	volume, object, err := storageNativeDarwinMetadata(context.Background(), nil)
	if volume != (storageNativeDarwinVolumeRaw{}) || object != (storageNativeDarwinObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %+v %v", volume, object, err)
	}
}

func TestStorageNativeDarwinTypedBridgeUnavailable(t *testing.T) {
	got, err := storageNativeDarwinObjectMetadata(context.Background(), nil)
	if got != (storageNativeDarwinTypedObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %v", got, err)
	}
}
