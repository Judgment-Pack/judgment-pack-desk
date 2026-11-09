//go:build !darwin || !cgo || (!amd64 && !arm64)

package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Current CGO_ENABLED=0 archives cannot supply the public attribute bridge.
// This usable refusal is not a healthy production observer or a raw syscall.
func storageNativeDarwinMetadata(context.Context, *os.File) (storageNativeDarwinVolumeRaw, storageNativeDarwinObjectRaw, error) {
	return storageNativeDarwinVolumeRaw{}, storageNativeDarwinObjectRaw{}, storagebudget.ErrUnsupported
}

func storageNativeDarwinObjectMetadata(context.Context, *os.File) (storageNativeDarwinTypedObjectRaw, error) {
	return storageNativeDarwinTypedObjectRaw{}, storagebudget.ErrUnsupported
}
