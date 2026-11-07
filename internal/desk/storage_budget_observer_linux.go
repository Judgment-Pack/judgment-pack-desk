//go:build linux

package desk

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageAdvisoryPlatformProfiles(profiles []storageOwnerProfile) error {
	for _, p := range profiles {
		if p.ID != 4097 || !storageOwnerAdvisoryProfileValid(p) {
			return storagebudget.ErrUnsupported
		}
	}
	return nil
}

// ReadHeld is a native fact leaf shared with sealed original-launch provision.
// The caller supplies and retains normal original descriptor custody; the leaf
// grants no launch authority and performs no filesystem effects.
func storageAdvisoryLinuxHeldBase(ctx context.Context, held *os.File) (storageAdvisoryNativeFacts, error) {
	s, err := storageNativeStatfs(ctx, held)
	if err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	if s.Type != 0xef53 {
		return storageAdvisoryNativeFacts{}, storagebudget.ErrUnsupported
	}
	b, err := storageNativeLinuxBlockBytes(s)
	if err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	n, err := storageNativeFiniteInodes(s)
	if err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	if _, err = storageNativeExt4(ctx, held); err != nil {
		return storageAdvisoryNativeFacts{}, err
	}
	status := storageAdvisoryStatus(s.Started)
	status.Bytes = storagePhysicalCounterMeasured
	status.Inodes = storagePhysicalCounterMeasured
	status.APFSContainer = storagePhysicalDiscoveryNotApplicable
	return storageAdvisoryNativeFacts{status: status, totalBytes: b.Total, available: storagebudget.CapacityVector{Bytes: b.Available, Inodes: n}}, nil
}
func storageAdvisoryNativeView(ctx context.Context, held *os.File, fs storageOwnerFilesystem, m storageOwnerMembership, rows map[storagebudget.ID]storageOwnerConstraint) (storageAdvisoryNativeFacts, error) {
	out, err := storageAdvisoryLinuxHeldBase(ctx, held)
	if err != nil {
		return out, err
	}
	if fs.Backend != 1 || m.Backend != 1 || m.ProfileID != 4097 || m.ProfileVersion != 2 || fs.Definition.InodeModel != storagebudget.InodesFinite {
		return out, storagebudget.ErrUnsupported
	}
	ext, err := storageNativeExt4(ctx, held)
	if err != nil {
		return out, err
	}
	if ext.UUID != fs.VolumeUUID || ext.UUID != m.VolumeUUID {
		return out, errStorageCustody
	}
	object, err := storageNativeLinuxObject(ctx, held)
	if err != nil {
		return out, err
	}
	subjects := []storageOwnerSubject{m.User, m.Group, m.Project}
	ids, err := storageAdvisoryExt4QuotaSubjects(object, ext, uint32(os.Geteuid()), uint32(os.Getegid()), storageAdvisoryProspectiveChild)
	if err != nil {
		return out, err
	}
	if err = storageAdvisoryValidateQuotaSubjects(m, ids); err != nil {
		return out, err
	}
	statuses := []*storagePhysicalDiscovery{&out.status.UserQuota, &out.status.GroupQuota, &out.status.ProjectQuota}
	for i, subject := range subjects {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		kind := storageNativeQuotaKind(i)
		info, ie := storageNativeQuotaInfo(ctx, held, kind)
		if errors.Is(ie, errStorageNativeQuotaDisabled) {
			*statuses[i] = storagePhysicalDiscoveryDisabled
			if subject.State == 3 {
				return out, storagebudget.ErrUnknown
			}
			continue
		}
		if ie != nil {
			if subject.State == 3 {
				return out, errors.Join(storagebudget.ErrUnknown, ie)
			}
			continue
		}
		if !info.Started.IsZero() && info.Started.Before(out.status.Oldest) {
			out.status.Oldest = info.Started
		}
		state, se := storageNativeQuotaStatus(ctx, held, kind)
		if se == nil {
			v, ve := state.state()
			if ve == nil {
				switch v {
				case storageNativeQuotaStateDisabled:
					*statuses[i] = storagePhysicalDiscoveryDisabled
				case storageNativeQuotaStateAccounting:
					*statuses[i] = storagePhysicalDiscoveryAccounting
				case storageNativeQuotaStateEnforced:
					*statuses[i] = storagePhysicalDiscoveryEnforced
				}
			}
		}
		// Positive subject mapping belongs to original enrolled membership. Unknown
		// discovery never creates a subject-zero claim or a fabricated exact row.
		id := ids[i]
		if subject.State == 3 {
			if subject.Subject != id {
				return out, errStorageOwnerTransitionRequired
			}
			id = subject.Subject
		}
		raw, re := storageNativeQuotaSubjectQuery(ctx, held, storageNativeQuotaSubject{Kind: kind, ID: id})
		if re != nil {
			if subject.State == 3 {
				return out, errors.Join(storagebudget.ErrUnknown, re)
			}
			continue
		}
		remaining, re := storageNativeQuotaExt4Remaining(raw)
		if re != nil {
			if subject.State == 3 || raw.HardBlocks != 0 || raw.SoftBlocks != 0 || raw.HardInodes != 0 || raw.SoftInodes != 0 {
				return out, errors.Join(storagebudget.ErrUnknown, re)
			}
			continue
		}
		finite := remaining.Bytes.State == storageNativeQuotaFinite || remaining.Inodes.State == storageNativeQuotaFinite
		if subject.State != 3 {
			if finite {
				return out, errStorageOwnerTransitionRequired
			}
			continue
		}
		row, ok := rows[subject.Constraint]
		if !ok || row.Subject != subject || row.Pool != fs.Definition.ID || row.VolumeUUID != ext.UUID {
			return out, storagebudget.ErrUnknown
		}
		if remaining.Bytes.State != storageNativeQuotaFinite || remaining.Inodes.State != storageNativeQuotaFinite {
			return out, storagebudget.ErrUnknown
		}
		at := raw.Started
		if at.IsZero() {
			return out, storagebudget.ErrUnknown
		}
		if at.Before(out.status.Oldest) {
			out.status.Oldest = at
		}
		out.exact = append(out.exact, storagebudget.CapacityObservation{Constraint: row.Definition.ID, Generation: row.Definition.Generation, KnownResources: row.Definition.Resources, Available: storagebudget.CapacityVector{Bytes: remaining.Bytes.Available, Inodes: remaining.Inodes.Available}, At: at})
	}
	currentExt, ce := storageNativeExt4(ctx, held)
	if ce != nil {
		return out, ce
	}
	currentObject, ce := storageNativeLinuxObject(ctx, held)
	if ce != nil {
		return out, ce
	}
	if currentExt.UUID != fs.VolumeUUID || currentExt.UUID != m.VolumeUUID {
		return out, errStorageCustody
	}
	currentIDs, ce := storageAdvisoryExt4QuotaSubjects(currentObject, currentExt, uint32(os.Geteuid()), uint32(os.Getegid()), storageAdvisoryProspectiveChild)
	if ce != nil {
		return out, ce
	}
	if ce = storageAdvisoryConfirmQuotaSubjects(m, ids, currentIDs); ce != nil {
		return out, ce
	}
	if time.Since(out.status.Oldest) > time.Duration(storageAdvisoryInitialMaxAgeNS) {
		return out, storagebudget.ErrStale
	}
	return out, nil
}

func storageAdvisoryPersistentHeld(ctx context.Context, held *os.File) (storageOwnerPersistentObject, error) {
	ext, err := storageNativeExt4Object(ctx, held)
	if err != nil {
		return storageOwnerPersistentObject{}, err
	}
	obj, err := storageNativeLinuxObjectMetadata(ctx, held)
	if err != nil {
		return storageOwnerPersistentObject{}, err
	}
	return storageOwnerPersistentObject{Backend: 1, VolumeUUID: ext.UUID, Object: obj.Inode, Incarnation: uint64(ext.InodeGeneration), IncarnationKind: 1, ObjectKind: ext.Kind}, nil
}

// Linux already obtains the volume and incarnation from the actual target.
// The caller retains and checks the original parent and target descriptions.
func storageAdvisoryPersistentFileHeld(ctx context.Context, parent, held *os.File) (storageOwnerPersistentObject, error) {
	if parent == nil {
		return storageOwnerPersistentObject{}, os.ErrInvalid
	}
	return storageAdvisoryPersistentHeld(ctx, held)
}

// Pre-Q launch has no enrolled exact class rows. Unavailable discovery stays
// Unknown; a positively measured finite restriction requires the separate exact
// launch allowance route and refuses here rather than being omitted.
func storageAdvisoryReadHeld(ctx context.Context, held *os.File) (storageAdvisoryNativeFacts, error) {
	return storageAdvisoryReadLinuxHeld(ctx, held, storageAdvisoryProspectiveChild)
}

// The startup owner uses this for parent/target directory metadata effects.
// A regular-file target requires its own exact object-to-query-directory join;
// this directory leaf does not certify a regular file by sampling its parent.
func storageAdvisoryReadExistingHeld(ctx context.Context, held *os.File) (storageAdvisoryNativeFacts, error) {
	return storageAdvisoryReadLinuxHeld(ctx, held, storageAdvisoryExistingObject)
}
func storageAdvisoryReadLinuxHeld(ctx context.Context, held *os.File, use storageAdvisoryObjectUse) (storageAdvisoryNativeFacts, error) {
	out, err := storageAdvisoryLinuxHeldBase(ctx, held)
	if err != nil {
		return out, err
	}
	object, err := storageNativeLinuxObject(ctx, held)
	if err != nil {
		return out, err
	}
	ext, err := storageNativeExt4(ctx, held)
	if err != nil {
		return out, err
	}
	ids, err := storageAdvisoryExt4QuotaSubjects(object, ext, uint32(os.Geteuid()), uint32(os.Getegid()), use)
	if err != nil {
		return out, err
	}
	out, err = storageAdvisoryReadLinuxQuotaSubjects(ctx, held, out, ids)
	if err != nil {
		return out, err
	}
	currentExt, err := storageNativeExt4(ctx, held)
	if err != nil {
		return out, err
	}
	currentObject, err := storageNativeLinuxObject(ctx, held)
	if err != nil {
		return out, err
	}
	if currentExt.UUID != ext.UUID {
		return out, errStorageCustody
	}
	currentIDs, err := storageAdvisoryExt4QuotaSubjects(currentObject, currentExt, uint32(os.Geteuid()), uint32(os.Getegid()), use)
	if err != nil {
		return out, err
	}
	return out, storageAdvisoryConfirmQuotaSubjects(storageOwnerMembership{}, ids, currentIDs)
}
func storageAdvisoryReadLinuxQuotaSubjects(ctx context.Context, held *os.File, out storageAdvisoryNativeFacts, ids [3]uint32) (storageAdvisoryNativeFacts, error) {
	states := []*storagePhysicalDiscovery{&out.status.UserQuota, &out.status.GroupQuota, &out.status.ProjectQuota}
	for i, id := range ids {
		info, qe := storageNativeQuotaInfo(ctx, held, storageNativeQuotaKind(i))
		if errors.Is(qe, errStorageNativeQuotaDisabled) {
			*states[i] = storagePhysicalDiscoveryDisabled
			continue
		}
		if qe != nil {
			continue
		}
		if info.Started.Before(out.status.Oldest) {
			out.status.Oldest = info.Started
		}
		raw, qe := storageNativeQuotaSubjectQuery(ctx, held, storageNativeQuotaSubject{Kind: storageNativeQuotaKind(i), ID: id})
		if qe != nil {
			continue
		}
		if raw.Started.Before(out.status.Oldest) {
			out.status.Oldest = raw.Started
		}
		if qe = storageAdvisoryLaunchQuotaRefusal(raw); qe != nil {
			return out, qe
		}
	}
	return out, nil
}

// The parent is only a quota-query/counter description. Subject selection comes
// from the exact retained regular target, never from the directory's project or
// from prospective inheritance. Both descriptions remain borrowed throughout.
func storageAdvisoryReadExistingLeaseHeld(ctx context.Context, parent, lease *os.File) (storageAdvisoryNativeFacts, error) {
	out, err := storageAdvisoryLinuxHeldBase(ctx, parent)
	if err != nil {
		return out, err
	}
	parentExt, err := storageNativeExt4(ctx, parent)
	if err != nil {
		return out, err
	}
	parentObject, err := storageNativeLinuxObject(ctx, parent)
	if err != nil {
		return out, err
	}
	targetExt, err := storageNativeExt4Object(ctx, lease)
	if err != nil {
		return out, err
	}
	targetObject, err := storageNativeLinuxObjectMetadata(ctx, lease)
	if err != nil {
		return out, err
	}
	if targetExt.Kind != storageNativeObjectRegular {
		return out, storagebudget.ErrAuthority
	}
	if err = storageAdvisoryLinuxTargetJoin(parentObject, targetObject, parentExt.UUID, targetExt.UUID); err != nil {
		return out, err
	}
	ids, err := storageAdvisoryExt4QuotaSubjects(targetObject, storageNativeExt4Raw{ParentProjectID: targetExt.ProjectID, ParentXFlags: targetExt.XFlags}, 0, 0, storageAdvisoryExistingObject)
	if err != nil {
		return out, err
	}
	out, err = storageAdvisoryReadLinuxQuotaSubjects(ctx, parent, out, ids)
	if err != nil {
		return out, err
	}
	afterExt, err := storageNativeExt4Object(ctx, lease)
	if err != nil {
		return out, err
	}
	afterObject, err := storageNativeLinuxObjectMetadata(ctx, lease)
	if err != nil {
		return out, err
	}
	afterParent, err := storageNativeLinuxObject(ctx, parent)
	if err != nil {
		return out, err
	}
	if err = storageAdvisoryLinuxTargetJoin(afterParent, afterObject, parentExt.UUID, afterExt.UUID); err != nil {
		return out, err
	}
	targetExt.Started = time.Time{}
	afterExt.Started = time.Time{}
	targetObject.Started = time.Time{}
	afterObject.Started = time.Time{}
	if targetExt != afterExt || targetObject != afterObject {
		return out, storagebudget.ErrUnknown
	}
	return out, nil
}
