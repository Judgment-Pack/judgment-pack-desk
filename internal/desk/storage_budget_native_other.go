//go:build !linux && !darwin

package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageNativeStatfs(context.Context, *os.File) (storageNativeStatfsRaw, error) {
	return storageNativeStatfsRaw{}, storagebudget.ErrUnsupported
}
