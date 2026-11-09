//go:build !linux || !amd64

package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// No unqualified ioctl ABI is guessed for other architectures/platforms.
func storageNativeExt4(context.Context, *os.File) (storageNativeExt4Raw, error) {
	return storageNativeExt4Raw{}, storagebudget.ErrUnsupported
}

func storageNativeExt4Object(context.Context, *os.File) (storageNativeExt4ObjectRaw, error) {
	return storageNativeExt4ObjectRaw{}, storagebudget.ErrUnsupported
}
