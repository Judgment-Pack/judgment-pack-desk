package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Consecutive original-current frames must see new cancellation, owner names,
// root identities and domain witnesses. Coalescing repeated installation reads
// inside one read-only frame must not cache authority for the next frame.
func TestStorageEnrollmentCurrentFrameFreshOriginalWitnesses(t *testing.T) {
	s := storageHistoryConcurrentServer(t)
	c := s.storageConstruction
	p, err := c.beginEnrollmentEpoch(t.Context())
	storageTestMust(t, err)
	defer func() { storageTestMust(t, p.finish(nil)) }()
	storageTestMust(t, p.acquire(t.Context()))
	storageTestMust(t, p.current(t.Context()))

	t.Run("canceled-caller", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := p.current(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal("read-only frame lost typed caller cancellation")
		}
		storageTestMust(t, p.current(t.Context()))
	})
	t.Run("substituted-owner-control", func(t *testing.T) {
		path := filepath.Join(s.configDir, storageUseName)
		saved := filepath.Join(s.configDir, "frame-original-storage-use")
		storageTestMust(t, os.Rename(path, saved))
		defer func() {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error(err)
			}
			storageTestMust(t, os.Rename(saved, path))
		}()
		storageTestMust(t, os.WriteFile(path, nil, 0600))
		if err := p.current(t.Context()); !errors.Is(err, errStorageCustody) {
			t.Fatal("next frame accepted a substituted original use control")
		}
	})
	storageTestMust(t, p.current(t.Context()))
	var root *storageRootBinding
	for _, h := range p.holds {
		if h.root != nil && !h.root.qControl && !h.root.domainDirectory {
			root = h.root
			break
		}
	}
	if root == nil {
		t.Fatal("actual startup has no original file-domain binding")
	}
	t.Run("root-definition", func(t *testing.T) {
		original := root.directory
		defer func() { root.directory = original }()
		root.directory.Inode++
		if err := p.current(t.Context()); !errors.Is(err, errStorageCustody) {
			t.Fatal("next frame accepted a different original root identity")
		}
	})
	storageTestMust(t, p.current(t.Context()))
	t.Run("domain-witness", func(t *testing.T) {
		original := root.domain.identity
		defer func() { root.domain.identity = original }()
		other, err := p.project.Stat()
		storageTestMust(t, err)
		root.domain.identity = other
		if err := p.current(t.Context()); !errors.Is(err, errStorageCustody) {
			t.Fatal("next frame accepted a different original domain witness")
		}
	})
	storageTestMust(t, p.current(t.Context()))
}
