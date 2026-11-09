//go:build !linux || !amd64

package desk

import (
	"context"
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeQuotaUnsupportedPlatform(t *testing.T) {
	requireStorageLine(t)
	ctx := context.Background()
	status, err := storageNativeQuotaStatus(ctx, nil, 0)
	if status != (storageNativeQuotaStatusRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %v", status, err)
	}
	info, err := storageNativeQuotaInfo(ctx, nil, 0)
	if info != (storageNativeQuotaInfoRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %v", info, err)
	}
	disk, err := storageNativeQuotaSubjectQuery(ctx, nil, storageNativeQuotaSubject{0, 42})
	if disk != (storageNativeQuotaDiskRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
		t.Fatalf("%+v %v", disk, err)
	}
}
