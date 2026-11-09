package desk

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageDarwinPersistentFixture struct {
	volume [2]storageNativeDarwinVolumeRaw
	raw    [2]storageNativeDarwinObjectRaw
	parent [2]storageNativeDarwinTypedObjectRaw
	target [2]storageNativeDarwinTypedObjectRaw
}

func storageDarwinPersistentHandwritten() storageDarwinPersistentFixture {
	volume := storageNativeDarwinVolumeRaw{UUID: [16]byte{7}}
	raw := storageNativeDarwinObjectRaw{FileID: 11, LinkID: 12, RealDevice: 13, RealFSID: [2]int32{14, 15}}
	parent := storageNativeDarwinTypedObjectRaw{Kind: storageNativeObjectDirectory, VnodeType: 2, FileID: 11, LinkID: 12, RealDevice: 13, RealFSID: [2]int32{14, 15}}
	target := storageNativeDarwinTypedObjectRaw{Kind: storageNativeObjectRegular, VnodeType: 1, FileID: 21, LinkID: 22, RealDevice: 13, RealFSID: [2]int32{14, 15}}
	return storageDarwinPersistentFixture{[2]storageNativeDarwinVolumeRaw{volume, volume}, [2]storageNativeDarwinObjectRaw{raw, raw}, [2]storageNativeDarwinTypedObjectRaw{parent, parent}, [2]storageNativeDarwinTypedObjectRaw{target, target}}
}

func storageDarwinPersistentRead(t *testing.T, ctx context.Context, f storageDarwinPersistentFixture, failAt int, failErr error, after func(int)) (storageOwnerPersistentObject, error) {
	t.Helper()
	// These distinct tokens are never used for native I/O. The callbacks must
	// observe the exact descriptions supplied, never a pathname or a new FD.
	parent, target := new(os.File), new(os.File)
	calls, dirs, parents, targets := 0, 0, 0, 0
	step := func(want string) error {
		calls++
		if after != nil {
			after(calls)
		}
		if calls > 6 || [...]string{"directory", "parent", "target", "target", "parent", "directory"}[calls-1] != want {
			t.Fatalf("unexpected native reader order at %d: %s", calls, want)
		}
		if calls == failAt {
			return failErr
		}
		return nil
	}
	p, err := storageAdvisoryDarwinPersistentFileWith(ctx, parent, target,
		func(_ context.Context, held *os.File) (storageNativeDarwinVolumeRaw, storageNativeDarwinObjectRaw, error) {
			if held != parent || held == target {
				t.Fatal("directory metadata queried on regular file or substituted description")
			}
			if e := step("directory"); e != nil {
				return storageNativeDarwinVolumeRaw{}, storageNativeDarwinObjectRaw{}, e
			}
			i := dirs
			dirs++
			return f.volume[i], f.raw[i], nil
		},
		func(_ context.Context, held *os.File) (storageNativeDarwinTypedObjectRaw, error) {
			switch held {
			case parent:
				if e := step("parent"); e != nil {
					return storageNativeDarwinTypedObjectRaw{}, e
				}
				i := parents
				parents++
				return f.parent[i], nil
			case target:
				if e := step("target"); e != nil {
					return storageNativeDarwinTypedObjectRaw{}, e
				}
				i := targets
				targets++
				return f.target[i], nil
			default:
				t.Fatal("typed metadata queried on substituted description")
				return storageNativeDarwinTypedObjectRaw{}, os.ErrInvalid
			}
		})
	if err == nil && (calls != 6 || dirs != 2 || parents != 2 || targets != 2) {
		t.Fatalf("incomplete successful native checks: %d/%d/%d/%d", calls, dirs, parents, targets)
	}
	return p, err
}

func TestStorageDarwinPersistentFileHandwritten(t *testing.T) {
	f := storageDarwinPersistentHandwritten()
	// Observation times and free-space counters can change without changing
	// native identity. They must not turn matching original objects into a miss.
	f.volume[1].Free = 91
	f.volume[1].Started = time.Unix(1, 0)
	f.raw[1].Started = time.Unix(2, 0)
	f.parent[1].Started = time.Unix(3, 0)
	f.target[1].Started = time.Unix(4, 0)
	got, err := storageDarwinPersistentRead(t, t.Context(), f, 0, nil, nil)
	want := storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte{7}, Object: 21, Incarnation: 22, IncarnationKind: 2, ObjectKind: storageNativeObjectRegular}
	if err != nil || got != want {
		t.Fatalf("regular original identity: got %+v, %v; want %+v", got, err, want)
	}
}

func TestStorageDarwinPersistentFileMismatchHandwritten(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*storageDarwinPersistentFixture)
		want   error
	}{
		{"parent-regular", func(f *storageDarwinPersistentFixture) { f.parent[0].Kind = storageNativeObjectRegular }, errStorageCustody},
		{"target-directory", func(f *storageDarwinPersistentFixture) { f.target[0].Kind = storageNativeObjectDirectory }, errStorageCustody},
		{"raw-parent-id", func(f *storageDarwinPersistentFixture) { f.raw[0].FileID++ }, errStorageCustody},
		{"raw-parent-link", func(f *storageDarwinPersistentFixture) { f.raw[0].LinkID++ }, errStorageCustody},
		{"raw-parent-device", func(f *storageDarwinPersistentFixture) { f.raw[0].RealDevice++ }, errStorageCustody},
		{"raw-parent-fsid", func(f *storageDarwinPersistentFixture) { f.raw[0].RealFSID[0]++ }, errStorageCustody},
		{"zero-fsid", func(f *storageDarwinPersistentFixture) {
			f.raw[0].RealFSID = [2]int32{}
			f.parent[0].RealFSID = [2]int32{}
		}, errStorageCustody},
		{"target-device", func(f *storageDarwinPersistentFixture) { f.target[0].RealDevice++ }, errStorageCustody},
		{"target-fsid", func(f *storageDarwinPersistentFixture) { f.target[0].RealFSID[1]++ }, errStorageCustody},
		{"after-parent-raw", func(f *storageDarwinPersistentFixture) { f.raw[1].FileID++ }, errStorageCustody},
		{"after-target-device", func(f *storageDarwinPersistentFixture) { f.target[1].RealDevice++ }, errStorageCustody},
		{"after-target-fsid", func(f *storageDarwinPersistentFixture) { f.target[1].RealFSID[0]++ }, errStorageCustody},
		{"zero-volume", func(f *storageDarwinPersistentFixture) { f.volume[0].UUID = [16]byte{} }, storagebudget.ErrUnknown},
		{"changed-volume", func(f *storageDarwinPersistentFixture) { f.volume[1].UUID[0]++ }, storagebudget.ErrUnknown},
		{"changed-parent", func(f *storageDarwinPersistentFixture) { f.parent[1].FileID++; f.raw[1].FileID++ }, storagebudget.ErrUnknown},
		{"changed-target-id", func(f *storageDarwinPersistentFixture) { f.target[1].FileID++ }, storagebudget.ErrUnknown},
		{"changed-target-link", func(f *storageDarwinPersistentFixture) { f.target[1].LinkID++ }, storagebudget.ErrUnknown},
		{"zero-target-id", func(f *storageDarwinPersistentFixture) { f.target[0].FileID = 0; f.target[1].FileID = 0 }, storagebudget.ErrUnknown},
		{"zero-target-link", func(f *storageDarwinPersistentFixture) { f.target[0].LinkID = 0; f.target[1].LinkID = 0 }, storagebudget.ErrUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := storageDarwinPersistentHandwritten()
			tc.change(&f)
			got, err := storageDarwinPersistentRead(t, t.Context(), f, 0, nil, nil)
			if got != (storageOwnerPersistentObject{}) || !errors.Is(err, tc.want) {
				t.Fatalf("mismatched original metadata: %+v %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestStorageDarwinPersistentFileFailuresHandwritten(t *testing.T) {
	fault := errors.New("handwritten native failure")
	for at := 1; at <= 6; at++ {
		got, err := storageDarwinPersistentRead(t, t.Context(), storageDarwinPersistentHandwritten(), at, fault, nil)
		if got != (storageOwnerPersistentObject{}) || !errors.Is(err, fault) {
			t.Fatalf("native failure %d: %+v %v", at, got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	got, err := storageDarwinPersistentRead(t, ctx, storageDarwinPersistentHandwritten(), 0, nil, func(at int) {
		if at == 6 {
			cancel()
		}
	})
	if got != (storageOwnerPersistentObject{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled native observation: %+v %v", got, err)
	}
}

func TestStorageDarwinPersistentFileInvalidInputsHandwritten(t *testing.T) {
	parent, target := new(os.File), new(os.File)
	directory := func(context.Context, *os.File) (storageNativeDarwinVolumeRaw, storageNativeDarwinObjectRaw, error) {
		t.Fatal("invalid input reached directory metadata")
		return storageNativeDarwinVolumeRaw{}, storageNativeDarwinObjectRaw{}, nil
	}
	object := func(context.Context, *os.File) (storageNativeDarwinTypedObjectRaw, error) {
		t.Fatal("invalid input reached object metadata")
		return storageNativeDarwinTypedObjectRaw{}, nil
	}
	for _, tc := range []struct {
		ctx       context.Context
		parent    *os.File
		target    *os.File
		directory func(context.Context, *os.File) (storageNativeDarwinVolumeRaw, storageNativeDarwinObjectRaw, error)
		object    func(context.Context, *os.File) (storageNativeDarwinTypedObjectRaw, error)
	}{
		{nil, parent, target, directory, object},
		{t.Context(), nil, target, directory, object},
		{t.Context(), parent, nil, directory, object},
		{t.Context(), parent, target, nil, object},
		{t.Context(), parent, target, directory, nil},
	} {
		got, err := storageAdvisoryDarwinPersistentFileWith(tc.ctx, tc.parent, tc.target, tc.directory, tc.object)
		if got != (storageOwnerPersistentObject{}) || !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("invalid input accepted: %+v %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := storageAdvisoryDarwinPersistentFileWith(ctx, parent, target, directory, object)
	if got != (storageOwnerPersistentObject{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input accepted: %+v %v", got, err)
	}
}
