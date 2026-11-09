//go:build !linux && !darwin

package desk

import (
	"context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"os"
)

func storageAdvisoryPlatformProfiles([]storageOwnerProfile) error {
	return storagebudget.ErrUnsupported
}
func storageAdvisoryReadHeld(context.Context, *os.File) (storageAdvisoryNativeFacts, error) {
	return storageAdvisoryNativeFacts{}, storagebudget.ErrUnsupported
}
func storageAdvisoryNativeView(context.Context, *os.File, storageOwnerFilesystem, storageOwnerMembership, map[storagebudget.ID]storageOwnerConstraint) (storageAdvisoryNativeFacts, error) {
	return storageAdvisoryNativeFacts{}, storagebudget.ErrUnsupported
}

func storageAdvisoryPersistentHeld(context.Context, *os.File) (storageOwnerPersistentObject, error) {
	return storageOwnerPersistentObject{}, storagebudget.ErrUnsupported
}

func storageAdvisoryPersistentFileHeld(context.Context, *os.File, *os.File) (storageOwnerPersistentObject, error) {
	return storageOwnerPersistentObject{}, storagebudget.ErrUnsupported
}

func storageAdvisoryReadExistingHeld(context.Context, *os.File) (storageAdvisoryNativeFacts, error) {
	return storageAdvisoryNativeFacts{}, storagebudget.ErrUnsupported
}

func storageAdvisoryReadExistingLeaseHeld(context.Context, *os.File, *os.File) (storageAdvisoryNativeFacts, error) {
	return storageAdvisoryNativeFacts{}, storagebudget.ErrUnsupported
}
