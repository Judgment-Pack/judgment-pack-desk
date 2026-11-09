package codexbridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// ObserveStorage neither initializes a manager nor opens credential files. The
// callback borrows a separate directory description while Close is excluded.
func (l *LazyManager) ObserveStorage(ctx context.Context, visit func(*os.Root) error) error {
	if !l.mu.TryLock() {
		return ErrBusy
	}
	defer l.mu.Unlock()
	if l.closed || ctx.Err() != nil {
		return ErrUnavailable
	}
	if l.manager != nil {
		return l.manager.ObserveStorage(ctx, visit)
	}
	// Never call get/openProfile: those acquire a lease and create state.
	return ObserveRetainedStorage(ctx, l.options.ProfileDir, visit)
}

// ObserveRetainedStorage selects existing metadata only. It does not adopt a
// profile, acquire a lease, initialize credentials or create missing directories.
// Desk supplies only its closed registry-derived managed profile namespace.
func ObserveRetainedStorage(ctx context.Context, dir string, visit func(*os.Root) error) error {
	return observeStoragePath(ctx, dir, nil, visit)
}

func (m *Manager) ObserveStorage(ctx context.Context, visit func(*os.Root) error) error {
	if !m.mu.TryLock() {
		return ErrBusy
	}
	defer m.mu.Unlock()
	if m.closed || ctx.Err() != nil || m.profile == nil {
		return ErrUnavailable
	}
	root, err := m.profile.root.OpenRoot(".")
	if err != nil {
		return ErrProfile
	}
	defer root.Close()
	return observeStoragePath(ctx, m.profile.dir, root, visit)
}

// This is the same bounded held-path proof as Desk's observer: retain every
// parent/name/child edge through the callback, then recheck the complete chain.
// It adds no ownership or admission authority. The existing manager/account
// mutexes still exclude Close and profile initialization. An initialized caller
// observes its original description; a new name never replaces that authority.
func observeStoragePath(ctx context.Context, dir string, held *os.Root, visit func(*os.Root) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || len(dir) > 4096 {
		return ErrProfile
	}
	parts := strings.Split(strings.TrimPrefix(dir, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) == 0 || len(parts) > 64 {
		return ErrProfile
	}
	base, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return ErrProfile
	}
	roots := []*os.Root{base}
	names := []string{}
	identities := []os.FileInfo{}
	defer func() {
		for i := len(roots) - 1; i >= 0; i-- {
			roots[i].Close()
		}
	}()
	current := func() error {
		for i, name := range names {
			before, e := roots[i].Lstat(name)
			if e != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, identities[i]) {
				return ErrProfile
			}
			actual, e := roots[i+1].Stat(".")
			if e != nil || !os.SameFile(before, actual) {
				return ErrProfile
			}
			after, e := roots[i].Lstat(name)
			if e != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(after, actual) {
				return ErrProfile
			}
		}
		return nil
	}
	for _, name := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "" {
			return ErrProfile
		}
		parent := roots[len(roots)-1]
		before, e := parent.Lstat(name)
		if e != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return ErrProfile
		}
		child, e := parent.OpenRoot(name)
		if e != nil {
			return ErrProfile
		}
		roots = append(roots, child)
		names = append(names, name)
		identities = append(identities, before)
		actual, e := child.Stat(".")
		if e != nil || !os.SameFile(before, actual) {
			return ErrProfile
		}
	}
	selected := roots[len(roots)-1]
	info, e := selected.Stat(".")
	if e != nil || !privateInfo(info, true) {
		return ErrProfile
	}
	parentInfo, e := roots[len(roots)-2].Stat(".")
	if e != nil || !privateParent(parentInfo) {
		return ErrProfile
	}
	if held != nil {
		original, e := held.Stat(".")
		if e != nil || !os.SameFile(info, original) {
			return ErrProfile
		}
	} else {
		held = selected
	}
	if err := current(); err != nil {
		return err
	}
	err = visit(held)
	if check := current(); check != nil {
		return check
	}
	after, e := selected.Stat(".")
	if e != nil || !privateInfo(after, true) || !os.SameFile(info, after) {
		return ErrProfile
	}
	afterParent, e := roots[len(roots)-2].Stat(".")
	if e != nil || !privateParent(afterParent) || !os.SameFile(parentInfo, afterParent) {
		return ErrProfile
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
