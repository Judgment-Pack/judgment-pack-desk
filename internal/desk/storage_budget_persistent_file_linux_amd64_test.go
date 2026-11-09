//go:build linux && amd64

package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
	"golang.org/x/sys/unix"
)

// This integration check requires the actual supported ext4 native path. A
// missing prerequisite is a failure, never successful native qualification.
func TestStorageOwnerPersistentFileLinuxNative(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	assistantRunMust(t, err)
	defer root.Close()
	f, err := root.OpenFile("held", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	assistantRunMust(t, err)
	assistantRunMust(t, f.Close())
	n, err := storageOpenFile(root, "held", 0)
	assistantRunMust(t, err)
	defer n.file.Close()
	fd := n.file.Fd()
	before, err := n.file.Stat()
	assistantRunMust(t, err)

	t.Run("regular-identity-and-borrow", func(t *testing.T) {
		ext, err := storageNativeExt4Object(ctx, n.file)
		assistantRunMust(t, err)
		metadata, err := storageNativeLinuxObjectMetadata(ctx, n.file)
		assistantRunMust(t, err)
		want := storageOwnerPersistentObject{Backend: 1, VolumeUUID: ext.UUID, Object: metadata.Inode, Incarnation: uint64(ext.InodeGeneration), IncarnationKind: 1, ObjectKind: storageNativeObjectRegular}
		if ext.Kind != storageNativeObjectRegular {
			t.Fatalf("regular description classified as %d", ext.Kind)
		}
		for i := 0; i < 2; i++ {
			got, err := storageOwnerPersistentFile(ctx, n)
			assistantRunMust(t, err)
			if got != want {
				t.Fatalf("persistent regular identity: got %+v want %+v", got, want)
			}
			if n.file.Fd() != fd {
				t.Fatal("persistent read changed retained descriptor")
			}
			after, err := n.file.Stat()
			assistantRunMust(t, err)
			if !os.SameFile(before, after) {
				t.Fatal("persistent read changed retained object")
			}
			assistantRunMust(t, n.current())
		}
	})
	t.Run("directory-and-directory-only-guard", func(t *testing.T) {
		got, err := storageOwnerPersistentRoot(ctx, root)
		assistantRunMust(t, err)
		if got.ObjectKind != storageNativeObjectDirectory {
			t.Fatalf("persistent directory kind %d", got.ObjectKind)
		}
		if _, err := storageNativeLinuxObject(ctx, n.file); !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("directory-only reader accepted regular file: %v", err)
		}
	})
	t.Run("path-and-special-refusal", func(t *testing.T) {
		pathFD, err := unix.Open(filepath.Join(dir, "held"), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		assistantRunMust(t, err)
		pathFile := os.NewFile(uintptr(pathFD), "held O_PATH")
		defer pathFile.Close()
		if got, err := storageAdvisoryPersistentHeld(ctx, pathFile); got != (storageOwnerPersistentObject{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("O_PATH: %+v %v", got, err)
		}
		if got, err := storageNativeLinuxObjectMetadata(ctx, pathFile); got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, storagebudget.ErrUnsupported) {
			t.Fatalf("metadata O_PATH: %+v %v", got, err)
		}
		reader, writer, err := os.Pipe()
		assistantRunMust(t, err)
		defer reader.Close()
		defer writer.Close()
		if got, err := storageAdvisoryPersistentHeld(ctx, reader); got != (storageOwnerPersistentObject{}) || !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("special object: %+v %v", got, err)
		}
		if got, err := storageNativeLinuxObjectMetadata(ctx, reader); got != (storageNativeLinuxObjectRaw{}) || !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("metadata special object: %+v %v", got, err)
		}
	})
	t.Run("context-refusal", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		for _, tc := range []struct {
			ctx  context.Context
			want error
		}{{nil, os.ErrInvalid}, {cancelled, context.Canceled}} {
			if got, err := storageOwnerPersistentFile(tc.ctx, n); got != (storageOwnerPersistentObject{}) || !errors.Is(err, tc.want) {
				t.Fatalf("context refusal: %+v %v", got, err)
			}
		}
		if n.file.Fd() != fd {
			t.Fatal("context refusal changed retained descriptor")
		}
		assistantRunMust(t, n.current())
	})
	// Replace the name only after all reads; the original descriptor stays held.
	t.Run("named-replacement-refusal", func(t *testing.T) {
		assistantRunMust(t, root.Rename("held", "original"))
		replacement, err := root.OpenFile("held", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		assistantRunMust(t, err)
		defer replacement.Close()
		if got, err := storageOwnerPersistentFile(ctx, n); got != (storageOwnerPersistentObject{}) || !errors.Is(err, errStorageCustody) {
			t.Fatalf("replacement accepted: %+v %v", got, err)
		}
		after, err := n.file.Stat()
		assistantRunMust(t, err)
		if n.file.Fd() != fd || !os.SameFile(before, after) {
			t.Fatal("replacement refusal closed or changed retained object")
		}
	})
}
