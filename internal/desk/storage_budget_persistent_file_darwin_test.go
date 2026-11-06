//go:build darwin && cgo && (amd64 || arm64)

package desk

import (
	"errors"
	"os"
	"testing"
)

// This focused gate needs the actual supported native bridge and filesystem.
// Missing prerequisites fail; portable handwritten tests are not a native pass.
func TestStorageOwnerPersistentFileDarwinNative(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	created, err := root.OpenFile("held", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = created.Close(); err != nil {
		t.Fatal(err)
	}
	n, err := storageOpenFile(root, "held", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := n.file.Close(); err != nil {
			t.Error(err)
		}
	}()
	parent, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := parent.Close(); err != nil {
			t.Error(err)
		}
	}()
	stat, err := storageNativeStatfs(t.Context(), parent)
	if err != nil || stat.Type != 26 {
		t.Fatalf("supported APFS prerequisite: type %d, %v", stat.Type, err)
	}
	volume, _, err := storageNativeDarwinMetadata(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	object, err := storageNativeDarwinObjectMetadata(t.Context(), n.file)
	if err != nil {
		t.Fatal(err)
	}
	want := storageOwnerPersistentObject{Backend: 2, VolumeUUID: volume.UUID, Object: object.FileID, Incarnation: object.LinkID, IncarnationKind: 2, ObjectKind: storageNativeObjectRegular}
	fd := n.file.Fd()
	for i := 0; i < 2; i++ {
		got, err := storageOwnerPersistentFile(t.Context(), n)
		if err != nil || got != want || n.file.Fd() != fd {
			t.Fatalf("actual original regular identity: %+v %v, want %+v on original FD", got, err, want)
		}
		if err := n.current(); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = storageNativeDarwinMetadata(t.Context(), n.file); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("directory-only guard accepted regular description: %v", err)
	}
	if err := root.Rename("held", "original"); err != nil {
		t.Fatal(err)
	}
	replacement, err := root.OpenFile("held", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := storageOwnerPersistentFile(t.Context(), n); got != (storageOwnerPersistentObject{}) || !errors.Is(err, errStorageCustody) {
		t.Fatalf("named replacement accepted as original: %+v %v", got, err)
	}
}
