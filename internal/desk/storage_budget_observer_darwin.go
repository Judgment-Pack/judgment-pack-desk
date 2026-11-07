//go:build darwin

package desk

import (
	"context"
	"os"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageAdvisoryPlatformProfiles(profiles []storageOwnerProfile) error {
	for _, p := range profiles {
		if p.ID != 4098 || !storageOwnerAdvisoryProfileValid(p) {
			return storagebudget.ErrUnsupported
		}
	}
	return nil
}

// Base returns raw scope facts; it must not classify a positive restriction as
// a launch-only refusal before the enrolled owner can apply its persistent fence.
func storageAdvisoryDarwinHeldBase(ctx context.Context, held *os.File) (storageAdvisoryNativeFacts, storageNativeDarwinVolumeRaw, error) {
	s, err := storageNativeStatfs(ctx, held)
	if err != nil {
		return storageAdvisoryNativeFacts{}, storageNativeDarwinVolumeRaw{}, err
	}
	if s.Type != 26 {
		return storageAdvisoryNativeFacts{}, storageNativeDarwinVolumeRaw{}, storagebudget.ErrUnsupported
	}
	v, _, err := storageNativeDarwinMetadata(ctx, held)
	if err != nil {
		return storageAdvisoryNativeFacts{}, v, err
	}
	b, err := storageNativeDarwinBlockBytes(s)
	if err != nil {
		return storageAdvisoryNativeFacts{}, v, err
	}
	status := storageAdvisoryStatus(s.Started)
	if v.Started.Before(status.Oldest) {
		status.Oldest = v.Started
	}
	status.Bytes = storagePhysicalCounterMeasured
	status.Inodes = storagePhysicalCounterDynamic
	status.UserQuota = storagePhysicalDiscoveryNotApplicable
	status.GroupQuota = storagePhysicalDiscoveryNotApplicable
	status.ProjectQuota = storagePhysicalDiscoveryNotApplicable
	return storageAdvisoryNativeFacts{status: status, totalBytes: b.Total, available: storagebudget.CapacityVector{Bytes: b.Available}}, v, nil
}
func storageAdvisoryReadHeld(ctx context.Context, held *os.File) (storageAdvisoryNativeFacts, error) {
	out, v, err := storageAdvisoryDarwinHeldBase(ctx, held)
	if err != nil {
		return out, err
	}
	return out, storageAdvisoryAPFSRestriction(v, storageOwnerMembership{}, nil, false)
}
func storageAdvisoryReadExistingHeld(ctx context.Context, held *os.File) (storageAdvisoryNativeFacts, error) {
	// APFS has no applicable user/group/project quota subject for this route.
	// Creation and existing directory metadata retain the same raw scope view.
	return storageAdvisoryReadHeld(ctx, held)
}
func storageAdvisoryNativeView(ctx context.Context, held *os.File, fs storageOwnerFilesystem, m storageOwnerMembership, rows map[storagebudget.ID]storageOwnerConstraint) (storageAdvisoryNativeFacts, error) {
	out, v, err := storageAdvisoryDarwinHeldBase(ctx, held)
	if err != nil {
		return out, err
	}
	if fs.Backend != 2 || m.Backend != 2 || m.ProfileID != 4098 || m.ProfileVersion != 2 || fs.Definition.InodeModel != storagebudget.InodesDynamic {
		return out, storagebudget.ErrUnsupported
	}
	if v.UUID != fs.VolumeUUID || v.UUID != m.VolumeUUID {
		return out, errStorageCustody
	}
	return out, storageAdvisoryAPFSRestriction(v, m, rows, true)
}

func storageAdvisoryPersistentHeld(ctx context.Context, held *os.File) (storageOwnerPersistentObject, error) {
	volume, _, err := storageNativeDarwinMetadata(ctx, held)
	if err != nil {
		return storageOwnerPersistentObject{}, err
	}
	object, err := storageNativeDarwinObjectMetadata(ctx, held)
	if err != nil {
		return storageOwnerPersistentObject{}, err
	}
	return storageOwnerPersistentObject{Backend: 2, VolumeUUID: volume.UUID, Object: object.FileID, Incarnation: object.LinkID, IncarnationKind: 2, ObjectKind: object.Kind}, nil
}

// Directory-only volume metadata comes from the retained original parent;
// the regular object's identity is read only from its actual held description.
func storageAdvisoryPersistentFileHeld(ctx context.Context, parent, held *os.File) (storageOwnerPersistentObject, error) {
	return storageAdvisoryDarwinPersistentFileWith(ctx, parent, held, storageNativeDarwinMetadata, storageNativeDarwinObjectMetadata)
}

func storageAdvisoryReadExistingLeaseHeld(ctx context.Context, parent, lease *os.File) (storageAdvisoryNativeFacts, error) {
	parentObject, err := storageNativeDarwinObjectMetadata(ctx, parent)
	if err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	targetObject, err := storageNativeDarwinObjectMetadata(ctx, lease)
	if err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	if err = storageAdvisoryDarwinTargetJoin(parentObject, targetObject); err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	out, err := storageAdvisoryReadExistingHeld(ctx, parent)
	if err != nil {
		return out, err
	}
	afterParent, err := storageNativeDarwinObjectMetadata(ctx, parent)
	if err != nil {
		return out, err
	}
	afterTarget, err := storageNativeDarwinObjectMetadata(ctx, lease)
	if err != nil {
		return out, err
	}
	if err = storageAdvisoryDarwinTargetJoin(afterParent, afterTarget); err != nil {
		return out, err
	}
	parentObject.Started = time.Time{}
	targetObject.Started = time.Time{}
	afterParent.Started = time.Time{}
	afterTarget.Started = time.Time{}
	if parentObject != afterParent || targetObject != afterTarget {
		return out, storagebudget.ErrUnknown
	}
	return out, nil
}
