//go:build (!linux || !amd64) && (!darwin || (!arm64 && !amd64))

package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Other targets inherit no Linux or Darwin ABI or native qualification.
// Supported Darwin observation/write/persistence leaves are separate.
func storageEnrollmentNativeSystemFacts(context.Context) (storageEnrollmentNativeSystem, error) {
	return storageEnrollmentNativeSystem{}, storagebudget.ErrUnsupported
}
func storageEnrollmentNativeWrite(context.Context, *os.File, []byte, int64) error {
	return storagebudget.ErrUnsupported
}
func storageEnrollmentNativeDense(context.Context, *os.File, uint64) error {
	return storagebudget.ErrUnsupported
}

func storageEnrollmentNativeParentObservation(context.Context, *os.File) (storageEnrollmentNativeParentFacts, error) {
	return storageEnrollmentNativeParentFacts{}, storagebudget.ErrUnsupported
}

func storageEnrollmentNativeSync(context.Context, *os.File) error {
	return storagebudget.ErrUnsupported
}
