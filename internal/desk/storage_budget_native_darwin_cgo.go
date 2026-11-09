//go:build darwin && cgo && (amd64 || arm64)

package desk

/*
#include <sys/attr.h>
#include <sys/types.h>
#include <sys/vnode.h>
#include <unistd.h>
#include <uuid/uuid.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>

#define STORAGE_NATIVE_VOLUME_MASK (ATTR_VOL_SIZE | ATTR_VOL_SPACEFREE | ATTR_VOL_SPACEAVAIL | ATTR_VOL_SPACEUSED | ATTR_VOL_CAPABILITIES | ATTR_VOL_UUID | ATTR_VOL_QUOTA_SIZE | ATTR_VOL_RESERVED_SIZE)
#define STORAGE_NATIVE_EXTENDED_MASK (ATTR_CMNEXT_LINKID | ATTR_CMNEXT_REALDEVID | ATTR_CMNEXT_REALFSID)

// The Go parsers use these published ABI layouts, never a native C struct cast.
_Static_assert(sizeof(off_t) == 8, "off_t ABI");
_Static_assert(sizeof(attribute_set_t) == 20, "attribute_set_t ABI");
_Static_assert(sizeof(vol_capabilities_attr_t) == 32, "volume capabilities ABI");
_Static_assert(sizeof(uuid_t) == 16, "uuid_t ABI");
_Static_assert(sizeof(fsobj_type_t) == 4, "object type ABI");
_Static_assert(ATTR_CMN_OBJTYPE == 0x8U, "object type mask ABI");
_Static_assert(VREG == 1 && VDIR == 2, "vnode type ABI");
_Static_assert(ATTR_CMN_RETURNED_ATTRS == 0x80000000U, "returned mask ABI");
_Static_assert(ATTR_CMN_FILEID == 0x02000000U, "file ID ABI");
_Static_assert(STORAGE_NATIVE_VOLUME_MASK == 0x3086001cU, "volume mask ABI");
_Static_assert(STORAGE_NATIVE_EXTENDED_MASK == 0xd0U, "extended mask ABI");

static int storage_native_getattrs(int fd, int volume, void *output, size_t length) {
    struct attrlist attrs;
    memset(&attrs, 0, sizeof(attrs));
    attrs.bitmapcount = ATTR_BIT_MAP_COUNT;
    attrs.commonattr = ATTR_CMN_RETURNED_ATTRS;
    unsigned long options = FSOPT_REPORT_FULLSIZE | FSOPT_PACK_INVAL_ATTRS;
    if (volume == 1 && length == 120) {
        attrs.volattr = ATTR_VOL_INFO | STORAGE_NATIVE_VOLUME_MASK;
    } else if (volume == 0 && length == 52) {
        attrs.commonattr |= ATTR_CMN_FILEID;
        attrs.forkattr = STORAGE_NATIVE_EXTENDED_MASK;
        options |= FSOPT_ATTR_CMN_EXTENDED | FSOPT_RETURN_REALDEV;
    } else if (volume == 2 && length == 56) {
        attrs.commonattr |= ATTR_CMN_OBJTYPE | ATTR_CMN_FILEID;
        attrs.forkattr = STORAGE_NATIVE_EXTENDED_MASK;
        options |= FSOPT_ATTR_CMN_EXTENDED | FSOPT_RETURN_REALDEV;
    } else {
        return EINVAL;
    }
    memset(output, 0, length);
    if (fgetattrlist(fd, &attrs, output, length, options) != 0) {
        return errno;
    }
    return 0;
}
*/
import "C"

import (
	"context"
	"os"
	"time"
	"unsafe"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

// Public libSystem call only. The two requests share the exact held descriptor,
// but are not a simultaneous snapshot or custody/lifetime proof. A later owner
// binding must check named currentness, revocation and complete constraints.
func storageNativeDarwinMetadata(ctx context.Context, f *os.File) (storageNativeDarwinVolumeRaw, storageNativeDarwinObjectRaw, error) {
	var volume storageNativeDarwinVolumeRaw
	var object storageNativeDarwinObjectRaw
	started := time.Now()
	err := storageNativeControl(ctx, f, func(fd int) error {
		if err := storageNativeDirectoryFD(fd); err != nil {
			return err
		}
		var volumeBuffer [storageNativeVolumeRecordBytes]byte
		if code := C.storage_native_getattrs(C.int(fd), 1, unsafe.Pointer(&volumeBuffer[0]), C.size_t(len(volumeBuffer))); code != 0 {
			return storageNativeDarwinError(unix.Errno(code))
		}
		var err error
		volume, err = storageNativeDarwinVolumeRecord(volumeBuffer[:])
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		var objectBuffer [storageNativeObjectRecordBytes]byte
		if code := C.storage_native_getattrs(C.int(fd), 0, unsafe.Pointer(&objectBuffer[0]), C.size_t(len(objectBuffer))); code != 0 {
			return storageNativeDarwinError(unix.Errno(code))
		}
		object, err = storageNativeDarwinObjectRecord(objectBuffer[:])
		return err
	})
	if err != nil {
		return storageNativeDarwinVolumeRaw{}, storageNativeDarwinObjectRaw{}, err
	}
	volume.Started, object.Started = started, started
	return volume, object, nil
}

// Object-only: no volume query is made on a regular descriptor. Correlating it
// with retained volume evidence and original named custody is later owner work.
func storageNativeDarwinObjectMetadata(ctx context.Context, f *os.File) (storageNativeDarwinTypedObjectRaw, error) {
	var out storageNativeDarwinTypedObjectRaw
	started := time.Now()
	err := storageNativeControl(ctx, f, func(fd int) error {
		kind, err := storageNativeObjectFD(fd)
		if err != nil {
			return err
		}
		var buffer [storageNativeTypedObjectRecordBytes]byte
		if code := C.storage_native_getattrs(C.int(fd), 2, unsafe.Pointer(&buffer[0]), C.size_t(len(buffer))); code != 0 {
			return storageNativeDarwinError(unix.Errno(code))
		}
		out, err = storageNativeDarwinTypedObjectRecord(buffer[:])
		if err != nil {
			return err
		}
		if out.Kind != kind {
			return storagebudget.ErrUnknown
		}
		return nil
	})
	if err != nil {
		return storageNativeDarwinTypedObjectRaw{}, err
	}
	out.Started = started
	return out, nil
}
