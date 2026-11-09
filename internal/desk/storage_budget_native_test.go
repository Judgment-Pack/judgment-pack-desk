package desk

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageNativeByteNormalization(t *testing.T) {
	base := storageNativeStatfsRaw{BlockSize: 4096, FragmentSize: 1024, Blocks: 10, FreeBlocks: 4, AvailableBlocks: 3}
	for _, tc := range []struct {
		name   string
		change func(*storageNativeStatfsRaw)
		darwin bool
		want   storageNativeByteCounts
		err    error
	}{
		{"fragment-unit", nil, false, storageNativeByteCounts{10240, 3072}, nil},
		{"darwin-block-unit", nil, true, storageNativeByteCounts{40960, 12288}, nil},
		{"zero-fragment-fallback", func(s *storageNativeStatfsRaw) { s.FragmentSize = 0 }, false, storageNativeByteCounts{40960, 12288}, nil},
		{"exhausted", func(s *storageNativeStatfsRaw) { s.AvailableBlocks = 0 }, false, storageNativeByteCounts{10240, 0}, nil},
		{"negative-fragment", func(s *storageNativeStatfsRaw) { s.FragmentSize = -1 }, false, storageNativeByteCounts{}, storagebudget.ErrBounds},
		{"zero-unit", func(s *storageNativeStatfsRaw) { s.FragmentSize, s.BlockSize = 0, 0 }, false, storageNativeByteCounts{}, storagebudget.ErrBounds},
		{"darwin-unit-sentinel", func(s *storageNativeStatfsRaw) { s.BlockSize = math.MaxUint32 }, true, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"unknown-total", func(s *storageNativeStatfsRaw) { s.Blocks = 0 }, false, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"sentinel-total", func(s *storageNativeStatfsRaw) { s.Blocks = math.MaxUint64 }, false, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"sentinel-free", func(s *storageNativeStatfsRaw) { s.FreeBlocks = math.MaxUint64 }, false, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"sentinel-available", func(s *storageNativeStatfsRaw) { s.AvailableBlocks = math.MaxUint64 }, false, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"inconsistent-free", func(s *storageNativeStatfsRaw) { s.FreeBlocks = 11 }, false, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"inconsistent-available", func(s *storageNativeStatfsRaw) { s.AvailableBlocks = 5 }, false, storageNativeByteCounts{}, storagebudget.ErrUnknown},
		{"multiplication-overflow", func(s *storageNativeStatfsRaw) { s.Blocks = math.MaxUint64/1024 + 1 }, false, storageNativeByteCounts{}, storagebudget.ErrBounds},
		{"finite-format-bound", func(s *storageNativeStatfsRaw) { s.Blocks = (1<<60)/1024 + 1 }, false, storageNativeByteCounts{}, storagebudget.ErrBounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := base
			if tc.change != nil {
				tc.change(&raw)
			}
			var got storageNativeByteCounts
			var err error
			if tc.darwin {
				got, err = storageNativeDarwinBlockBytes(raw)
			} else {
				got, err = storageNativeLinuxBlockBytes(raw)
			}
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("counts=%+v err=%v; want %+v %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestStorageNativeFiniteInterpretation(t *testing.T) {
	for _, tc := range []struct {
		files, free, want uint64
		err               error
	}{
		{10, 3, 3, nil}, {10, 0, 0, nil}, {0, 0, 0, storagebudget.ErrUnknown},
		{math.MaxUint64, 0, 0, storagebudget.ErrUnknown}, {10, math.MaxUint64, 0, storagebudget.ErrUnknown},
		{10, 11, 0, storagebudget.ErrUnknown}, {1<<40 + 1, 1, 0, storagebudget.ErrBounds},
	} {
		got, err := storageNativeFiniteInodes(storageNativeStatfsRaw{Files: tc.files, FreeFiles: tc.free})
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Fatalf("finite inode result %d %v for %+v", got, err, tc)
		}
	}
	for _, tc := range []struct {
		limit, used, want uint64
		err               error
	}{
		{10, 3, 7, nil}, {10, 10, 0, nil}, {10, 11, 0, nil}, {0, 0, 0, nil},
		{1<<60 + 1, 0, 0, storagebudget.ErrBounds}, {1, 1<<60 + 1, 0, storagebudget.ErrBounds},
	} {
		got, err := storageNativeFiniteByteRemaining(tc.limit, tc.used)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Fatalf("finite remaining result %d %v for %+v", got, err, tc)
		}
	}
}

func TestStorageNativeControlRefusesMissingInputs(t *testing.T) {
	called := false
	query := func(int) error { called = true; return nil }
	if err := storageNativeControl(context.Background(), nil, query); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
	if err := storageNativeControl(nil, nil, nil); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
	if called {
		t.Fatal("query ran without a held descriptor")
	}
}

func TestStorageNativeControlBorrowAndCancellation(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "held")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	wantFD := f.Fd()
	calls := 0
	query := func(fd int) error {
		calls++
		if uintptr(fd) != wantFD {
			t.Fatal("different description supplied")
		}
		return nil
	}
	if err := storageNativeControl(context.Background(), f, query); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat(); err != nil {
		t.Fatalf("borrow closed original: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := storageNativeControl(ctx, f, query); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled query ran: %d %v", calls, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if err := storageNativeControl(ctx, f, func(int) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("accepted result after cancellation: %v", err)
	}
	if _, err := f.Stat(); err != nil {
		t.Fatalf("cancellation closed original: %v", err)
	}
}
