package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A second implementation verifies that workflows do not fall back to files.
// This is a test adapter, not a selectable cloud or persistent memory backend.
type memoryArtifacts struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryArtifacts() *memoryArtifacts { return &memoryArtifacts{data: map[string][]byte{}} }
func (m *memoryArtifacts) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ref.valid() {
		return nil, errRetainedArtifactChanged
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.data[ref.SHA256]
	if !ok {
		return nil, os.ErrNotExist
	}
	if err := verifyRetainedArtifact(ref, raw); err != nil {
		return nil, err
	}
	return bytes.Clone(raw), nil
}
func (m *memoryArtifacts) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return retainedArtifactRef{}, err
	}
	ref := artifactReference(raw)
	if !ref.valid() {
		return retainedArtifactRef{}, errRetainedArtifactChanged
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if prior, ok := m.data[ref.SHA256]; ok && !bytes.Equal(prior, raw) {
		return retainedArtifactRef{}, errRetainedArtifactChanged
	}
	m.data[ref.SHA256] = bytes.Clone(raw)
	return ref, nil
}
func assertNoArtifactFiles(t *testing.T, s *Server) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(s.projectDir, retainedArtifactDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("workflow bypassed artifact repository", err)
	}
}
func TestRetainedArtifactRepositoryContract(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			s, _ := newTestServer(t, false)
			if backend == "memory" {
				s.retainedArtifacts = newMemoryArtifacts()
			}
			repo := s.artifactRepository()
			ctx := t.Context()
			if _, err := repo.Get(ctx, artifactReference([]byte("absent"))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			assertNoArtifactFiles(t, s)
			for _, raw := range [][]byte{{}, {0, 255, 1, 2}, []byte(" \n{\"value\" : 1.00}\n")} {
				ref, err := repo.Put(ctx, raw)
				if err != nil || ref != artifactReference(raw) {
					t.Fatal(ref, err)
				}
				got, err := repo.Get(ctx, ref)
				if err != nil || !bytes.Equal(got, raw) {
					t.Fatal("changed bytes", err)
				}
				if len(got) > 0 {
					got[0] ^= 255
				}
				again, err := repo.Get(ctx, ref)
				if err != nil || !bytes.Equal(again, raw) {
					t.Fatal("snapshot aliases storage", err)
				}
				if repeated, err := repo.Put(ctx, raw); err != nil || repeated != ref {
					t.Fatal("repeat changed identity", err)
				}
				wrong := ref
				wrong.Bytes++
				if _, err := repo.Get(ctx, wrong); err == nil {
					t.Fatal("wrong size accepted")
				}
			}
			for _, ref := range []retainedArtifactRef{{SHA256: "../escape"}, {SHA256: strings.Repeat("f", 64), Bytes: -1}, {SHA256: strings.Repeat("f", 64), Bytes: retainedArtifactByteLimit + 1}} {
				if _, err := repo.Get(ctx, ref); err == nil {
					t.Fatal("invalid identity accepted")
				}
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := repo.Put(canceled, []byte("cancelled")); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := repo.Get(ctx, artifactReference([]byte("cancelled"))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("canceled put wrote data", err)
			}
			if _, err := repo.Put(ctx, make([]byte, retainedArtifactByteLimit+1)); err == nil {
				t.Fatal("oversized artifact accepted")
			}
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Go(func() {
					raw := []byte("concurrent content")
					if ref, err := repo.Put(ctx, raw); err != nil || ref != artifactReference(raw) {
						t.Error("concurrent put", err)
					}
				})
			}
			wg.Wait()
			if backend == "memory" {
				assertNoArtifactFiles(t, s)
			}
		})
	}
}
func TestRetainedArtifactFilesRefuseCorruptionAndCrossWorkspace(t *testing.T) {
	s, _ := newTestServer(t, false)
	other, _ := newTestServer(t, false)
	repo := s.artifactRepository()
	raw := []byte("immutable content")
	ref, err := repo.Put(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.projectDir, retainedArtifactDirectory, ref.SHA256)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Put(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("identical object replaced")
	}
	if _, err = other.artifactRepository().Get(t.Context(), ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cross-workspace artifact access", err)
	}
	assertNoArtifactFiles(t, other)
	if err = os.WriteFile(path, []byte("corrupted content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Get(t.Context(), ref); err == nil {
		t.Fatal("corruption accepted")
	}
	if _, err = repo.Put(t.Context(), raw); err == nil {
		t.Fatal("corruption overwritten")
	}
	damaged, _ := os.ReadFile(path)
	if string(damaged) != "corrupted content" {
		t.Fatal("put repaired evidence")
	}
}
func TestRetainedArtifactUncertainWriteIsNotAcknowledged(t *testing.T) {
	s, _ := newTestServer(t, false)
	repo := s.artifactRepository()
	raw := []byte("committed before read-back failed")
	expected := artifactReference(raw)
	reads := 0
	testHookPrivateRead = func(name string) error {
		if name == expected.SHA256 {
			reads++
			return errors.New("read-back unavailable")
		}
		return nil
	}
	defer func() { testHookPrivateRead = nil }()
	if ref, err := repo.Put(t.Context(), raw); err == nil || ref != (retainedArtifactRef{}) || reads != 1 {
		t.Fatal("uncertain persistence acknowledged or repeated", ref, err, reads)
	}
	testHookPrivateRead = nil
	got, err := repo.Get(t.Context(), expected)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("completed bytes lost", err)
	}
}
