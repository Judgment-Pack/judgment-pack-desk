//go:build !linux

package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageNativeLinuxObject(context.Context, *os.File) (storageNativeLinuxObjectRaw, error) {
	return storageNativeLinuxObjectRaw{}, storagebudget.ErrUnsupported
}

func storageNativeLinuxObjectMetadata(context.Context, *os.File) (storageNativeLinuxObjectRaw, error) {
	return storageNativeLinuxObjectRaw{}, storagebudget.ErrUnsupported
}
