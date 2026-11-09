package desk

import (
	"context"
	"os"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Explicit leaf arguments allow handwritten metadata/error vectors on any OS.
// Production supplies only the fixed native readers. Matching raw metadata is
// comparison material; the caller must retain original named-parent/file custody.
func storageAdvisoryDarwinPersistentFileWith(
	ctx context.Context,
	parent, held *os.File,
	directory func(context.Context, *os.File) (storageNativeDarwinVolumeRaw, storageNativeDarwinObjectRaw, error),
	object func(context.Context, *os.File) (storageNativeDarwinTypedObjectRaw, error),
) (storageOwnerPersistentObject, error) {
	var zero storageOwnerPersistentObject
	if ctx == nil || parent == nil || held == nil || directory == nil || object == nil {
		return zero, os.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	volume, parentRaw, err := directory(ctx, parent)
	if err != nil {
		return zero, err
	}
	parentObject, err := object(ctx, parent)
	if err != nil {
		return zero, err
	}
	if err = storageAdvisoryDarwinPersistentParentJoin(parentRaw, parentObject); err != nil {
		return zero, err
	}
	targetObject, err := object(ctx, held)
	if err != nil {
		return zero, err
	}
	if err = storageAdvisoryDarwinTargetJoin(parentObject, targetObject); err != nil {
		return zero, err
	}
	afterTarget, err := object(ctx, held)
	if err != nil {
		return zero, err
	}
	afterParent, err := object(ctx, parent)
	if err != nil {
		return zero, err
	}
	afterVolume, afterRaw, err := directory(ctx, parent)
	if err != nil {
		return zero, err
	}
	if err = storageAdvisoryDarwinPersistentParentJoin(afterRaw, afterParent); err != nil {
		return zero, err
	}
	if err = storageAdvisoryDarwinTargetJoin(afterParent, afterTarget); err != nil {
		return zero, err
	}
	parentObject.Started = time.Time{}
	afterParent.Started = time.Time{}
	targetObject.Started = time.Time{}
	afterTarget.Started = time.Time{}
	if volume.UUID == [16]byte{} || afterVolume.UUID != volume.UUID || parentObject != afterParent || targetObject != afterTarget {
		return zero, storagebudget.ErrUnknown
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	p := storageOwnerPersistentObject{Backend: 2, VolumeUUID: volume.UUID, Object: targetObject.FileID, Incarnation: targetObject.LinkID, IncarnationKind: 2, ObjectKind: targetObject.Kind}
	if !p.valid(storageNativeObjectRegular) {
		return zero, storagebudget.ErrUnknown
	}
	return p, nil
}

func storageAdvisoryDarwinPersistentParentJoin(raw storageNativeDarwinObjectRaw, typed storageNativeDarwinTypedObjectRaw) error {
	if typed.Kind != storageNativeObjectDirectory || raw.FileID != typed.FileID || raw.LinkID != typed.LinkID || raw.RealDevice != typed.RealDevice || raw.RealFSID != typed.RealFSID || raw.RealFSID == [2]int32{} {
		return errStorageCustody
	}
	return nil
}
