//go:build !linux || !amd64

package desk

import (
	"context"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// This batch qualifies no additional quota ABI or native platform. Existing
// released platforms remain required; refusal is not support completion.
func storageNativeQuotaStatus(context.Context, *os.File, storageNativeQuotaKind) (storageNativeQuotaStatusRaw, error) {
	return storageNativeQuotaStatusRaw{}, storagebudget.ErrUnsupported
}

func storageNativeQuotaInfo(context.Context, *os.File, storageNativeQuotaKind) (storageNativeQuotaInfoRaw, error) {
	return storageNativeQuotaInfoRaw{}, storagebudget.ErrUnsupported
}

func storageNativeQuotaSubjectQuery(context.Context, *os.File, storageNativeQuotaSubject) (storageNativeQuotaDiskRaw, error) {
	return storageNativeQuotaDiskRaw{}, storagebudget.ErrUnsupported
}
