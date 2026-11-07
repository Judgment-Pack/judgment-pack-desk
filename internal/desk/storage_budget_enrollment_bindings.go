package desk

import (
	"errors"
	"os"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// A construction-local binding image keeps candidate metadata out of ordinary
// owner state until its actual catalog/Q/census completion is reconciled. It
// borrows the original owner and sources; it is not another owner or registry.
// Existing immutable bindings are reused, including their domain witnesses.
type storageOwnerBindingImage struct {
	roots         map[storagebudget.ID]*storageRootBinding
	workspaces    map[storagebudget.ID]storagebudget.Workspace
	filesystems   map[storagebudget.ID]storagebudget.Filesystem
	constraints   map[storagebudget.ID]storagebudget.CapacityConstraint
	openedFiles   []*os.File
	installed     bool
	sourceAliases map[*storageOriginalSourceRoot]*storageOriginalSourceRoot
}

func newStorageOwnerBindingImage() *storageOwnerBindingImage {
	return &storageOwnerBindingImage{roots: map[storagebudget.ID]*storageRootBinding{}, workspaces: map[storagebudget.ID]storagebudget.Workspace{}, filesystems: map[storagebudget.ID]storagebudget.Filesystem{}, constraints: map[storagebudget.ID]storagebudget.CapacityConstraint{}}
}

func (b *storageOwnerBindingImage) Close() (err error) {
	if b == nil {
		return nil
	}
	for i := len(b.openedFiles) - 1; i >= 0; i-- {
		err = errors.Join(err, b.openedFiles[i].Close())
	}
	b.openedFiles = nil
	return err
}

func (b *storageOwnerBindingImage) install(o *storageBudgetOwner) error {
	if b == nil || b.installed || o == nil || !o.valid() {
		return storagebudget.ErrAuthority
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.closed {
		return os.ErrClosed
	}
	for id, old := range o.roots {
		if b.roots[id] != old {
			return errStorageCustody
		}
	}
	for id, old := range o.filesystems {
		if b.filesystems[id] != old {
			return errStorageCustody
		}
	}
	for id, old := range o.constraints {
		if b.constraints[id] != old {
			return errStorageCustody
		}
	}
	for id, old := range o.workspaces {
		next, ok := b.workspaces[id]
		if !ok || old.ID != next.ID || old.Generation != next.Generation || old.Limits != next.Limits {
			return errStorageCustody
		}
	}
	o.roots, o.filesystems, o.constraints = b.roots, b.filesystems, b.constraints
	// Keep existing workspace Known/Usage observations, which this immutable
	// image deliberately does not reconstruct from caller-provided counters.
	for id, old := range o.workspaces {
		b.workspaces[id] = old
	}
	o.workspaces = b.workspaces
	o.openedFiles = append(o.openedFiles, b.openedFiles...)
	b.openedFiles = nil
	b.installed = true
	return nil
}
