package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Scoped sources retain an original directory without owning all its contents.
// Callers first establish exact source/native currentness. Only the two closed,
// unowned source forms use the ordinary descriptor identity; owned ancestry and
// every ordinary private root retain storageDirectoryID's permission check.
func (s *storageOriginalSourceRoot) directoryIdentity() (storageDirectoryIdentity, error) {
	if s == nil || s.root == nil {
		return storageDirectoryIdentity{}, storagebudget.ErrAuthority
	}
	if s.scope != nil {
		project := s.role == 7 && s.scope.Default == nil && s.relative == ".desk-private" && bytes.Equal(s.scope.Namespace, []byte(".desk-private")) && s.parent == nil
		ancestor := s.role == 8 && s.scope.Default != nil && (s.scope.Default.Workflow == 2 || s.scope.Default.Workflow == 4) && s.scope.Default.OwnSelf == 0 && s.relative == string(s.scope.Default.Next) && storageEnrollmentDefaultSourceNamespace(*s.scope) && s.parent == nil
		if project || ancestor {
			info, err := s.root.Stat(".")
			if err != nil {
				return storageDirectoryIdentity{}, err
			}
			if !info.IsDir() {
				return storageDirectoryIdentity{}, errStorageCustody
			}
			return storageFileIdentity(info)
		}
	}
	return storageDirectoryID(s.root)
}

// Resolve only from the actual original adopted project or original home
// registry's retained project edges. A catalog pathname is never followed.
func (x *storageOriginalSources) exactProjectScope(ctx context.Context, catalog storageOwnerCatalogV2, row storageOwnerRoot) (*storageOriginalSourceRoot, error) {
	if x == nil || x.construction == nil || row.Role != 7 || row.Source.Kind != 7 || int(row.DirectoryObject) >= len(catalog.Objects) || !x.originalOwnerEqual(catalog) {
		return nil, storagebudget.ErrAuthority
	}
	binding, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
	if err != nil || binding.Project != catalog.Objects[row.DirectoryObject].Persistent || !storageEnrollmentPeakSourceJoins(binding, catalog, row) {
		return nil, errors.Join(errStorageCustody, err)
	}
	for _, prior := range x.roots {
		if prior.role == 7 && prior.persistent == binding.Project {
			if prior.scope == nil || !reflect.DeepEqual(*prior.scope, binding) {
				return nil, errStorageCustody
			}
			return prior, prior.current(ctx, x.construction)
		}
	}
	workspace := false
	for _, w := range x.workspaces {
		if w.Definition.ID == binding.Workspace && w.Definition.Generation == binding.WorkspaceGeneration && bytes.Equal(w.OwnerKey, binding.OwnerKey) && bytes.Equal(w.HistoryIdentity, binding.HistoryIdentity) {
			workspace = true
		}
	}
	if !workspace {
		return nil, errStorageCustody
	}
	var root *os.Root
	var path string
	var witnesses []storageNamedRoot
	if x.owner.Kind == 1 {
		key, err := x.construction.inspectOriginalStartupKey(ctx)
		if err != nil {
			joined := errors.Join(errStorageCustody, err)
			return nil, joined
		}
		if key != string(binding.OwnerKey) {
			joined := errors.Join(errStorageCustody, err)
			return nil, joined
		}
		history, historyErr := x.construction.originalStorageHistory(ctx)
		if historyErr != nil {
			return nil, historyErr
		}
		if history != string(binding.HistoryIdentity) {
			joined := errors.Join(errStorageCustody, err)
			return nil, joined
		}
		root, path = x.construction.project.own.root, x.construction.server.projectDir
	} else if x.owner.Kind == 2 {
		for _, original := range x.projects {
			if original.entry.RunnerWorkspace == string(binding.OwnerKey) && original.entry.HistoryIdentity == string(binding.HistoryIdentity) {
				if root != nil {
					return nil, errStorageCustody
				}
				if err := original.named.current(); err != nil {
					return nil, err
				}
				root = original.named.root
				path = filepath.Join(string(x.owner.HomePath), original.entry.Folder)
				witnesses = []storageNamedRoot{original.named}
			}
		}
	}
	if root == nil {
		return nil, errStorageCustody
	}
	native, err := storageOwnerPersistentRoot(ctx, root)
	if err != nil || native != binding.Project {
		return nil, errors.Join(errStorageCustody, err)
	}
	if len(x.roots) >= 4096 {
		return nil, storagebudget.ErrBounds
	}
	source := &storageOriginalSourceRoot{root: root, path: path, key: string(binding.OwnerKey), role: 7, category: storagebudget.Control, persistent: native, witnesses: witnesses, relative: ".desk-private", workspaces: append([]storagebudget.ID{}, row.References...), scope: &binding}
	x.roots = append(x.roots, source)
	return source, source.current(ctx, x.construction)
}

// A Kind8 child is independently captured by its original creator. On this
// launch the actual retained project edge and fixed child name must still name
// that exact captured native. Matching path/body alone is never adoption.
func (x *storageOriginalSources) exactCapturedPrivateChild(ctx context.Context, catalog storageOwnerCatalogV2, row storageOwnerRoot) (*storageOriginalSourceRoot, error) {
	if row.Role != 3 && row.Role != 6 || row.Source.Kind != 8 || !bytes.Equal(row.Source.Relative, []byte(".desk-private")) {
		return nil, storagebudget.ErrAuthority
	}
	var parent *storageOriginalSourceRoot
	for _, r := range catalog.Roots {
		if r.Definition.ID == row.Source.ParentRoot {
			var err error
			parent, err = x.exactProjectScope(ctx, catalog, r)
			if err != nil {
				return nil, err
			}
		}
	}
	if parent == nil {
		return nil, errStorageCustody
	}
	for _, source := range x.roots {
		if source.role != row.Role || source.persistent != catalog.Objects[row.DirectoryObject].Persistent {
			continue
		}
		if source.path != filepath.Join(parent.path, ".desk-private") {
			return nil, errStorageCustody
		}
		if len(source.witnesses) == 0 || source.witnesses[len(source.witnesses)-1].parent != parent.root {
			return nil, errStorageCustody
		}
		if err := source.current(ctx, x.construction); err != nil {
			return nil, err
		}
		return source, nil
	}
	return nil, errStorageCustody
}

// Count exactly the selected private subtree until a separately captured child
// root owns it. The adopted project root and all unrelated project entries are
// excluded. No directory enumeration of the project takes place here.
func (w *storageCensusWalk) projectPrivateScope(r *storageRootBinding, root *os.Root, count *int, usage *[storagebudget.Categories]storagebudget.Vector) (err error) {
	if r == nil || r.restrictedNamespace != ".desk-private" || r.source == nil || r.source.role != 7 || r.source.scope == nil || root != r.root {
		return storagebudget.ErrAuthority
	}
	if err = r.current(w.ctx); err != nil {
		return err
	}
	info, err := root.Lstat(".desk-private")
	if errors.Is(err, os.ErrNotExist) {
		// Recheck original custody after the actual positive absence read.
		return r.current(w.ctx)
	}
	if err != nil {
		return err
	}
	id, err := storageFileIdentity(info)
	if err != nil {
		return err
	}
	if child := w.roots[id]; child != nil && child != r {
		if child.source == nil || child.source.role != 3 && child.source.role != 6 || child.category != storagebudget.Control || child.source.path != filepath.Join(r.source.path, ".desk-private") || !reflect.DeepEqual(child.references, r.references) || child.scopeParent != (storagebudget.ID{}) && child.scopeParent != r.row.ID {
			return errStorageCustody
		}
		if len(child.source.witnesses) == 0 {
			return errStorageCustody
		}
		edge := child.source.witnesses[len(child.source.witnesses)-1]
		if edge.parent != root || edge.root != child.root || edge.name != ".desk-private" {
			return errStorageCustody
		}
		return errors.Join(child.current(w.ctx), r.current(w.ctx))
	}
	child, err := storagePinChild(root, ".desk-private", true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, child.current(), child.root.Close(), r.current(w.ctx)) }()
	return w.directory(r, child.root, []storageNamedRoot{child}, ".desk-private", 1, count, usage)
}

// An ancestry row is scoped to the next component of the platform-selected
// default path. It never enumerates the selected ancestor's other contents.
// Reopening compares original anchored catalog custody; it does not certify
// which mkdir produced a directory or discharge a pre-capture action.
func (x *storageOriginalSources) exactHistoryAncestorScope(ctx context.Context, catalog storageOwnerCatalogV2, row storageOwnerRoot) (_ *storageOriginalSourceRoot, err error) {
	if x == nil || x.construction == nil || row.Role != 8 || row.Source.Kind != 9 || int(row.DirectoryObject) >= len(catalog.Objects) || !x.originalOwnerEqual(catalog) {
		return nil, storagebudget.ErrAuthority
	}
	binding, err := storageEnrollmentPeakDecodeSource(row.Source.Relative)
	if err == nil && binding.Default != nil && binding.Default.Workflow == 4 {
		return x.exactCapturedTransferRoot(ctx, catalog, row)
	}
	if err != nil || binding.Default == nil || binding.Default.Workflow != 2 || binding.Default.Selected != catalog.Objects[row.DirectoryObject].Persistent || !storageEnrollmentPeakSourceJoins(binding, catalog, row) {
		return nil, errors.Join(errStorageCustody, err)
	}
	for _, old := range x.roots {
		if old.role == 8 && old.persistent == binding.Default.Selected {
			if old.scope == nil || !reflect.DeepEqual(*old.scope, binding) {
				return nil, errStorageCustody
			}
			return old, old.current(ctx, x.construction)
		}
	}
	if !x.historyAncestorSelected(ctx, binding) {
		return nil, errStorageCustody
	}
	walk, err := storageWalkHistoryAncestry(filepath.Join(string(binding.Default.SelectedPath), "history"))
	if err != nil {
		return nil, err
	}
	accepted := false
	defer func() {
		if !accepted {
			err = errors.Join(err, walk.Close())
		}
	}()
	if walk.path != string(binding.Default.SelectedPath) || walk.missing {
		return nil, errStorageCustody
	}
	native, err := storageOwnerPersistentRoot(ctx, walk.root())
	if err != nil || native != binding.Default.Selected {
		return nil, errors.Join(errStorageCustody, err)
	}
	if binding.Default.OwnSelf == 1 {
		info, e := walk.root().Stat(".")
		if e != nil || !storagePrivate(info, true) {
			return nil, errors.Join(errStorageCustody, e)
		}
	}
	if len(x.roots) >= 4096 {
		return nil, storagebudget.ErrBounds
	}
	source := &storageOriginalSourceRoot{root: walk.root(), witnesses: append([]storageNamedRoot{}, walk.edges...), path: string(binding.Default.SelectedPath), key: string(binding.OwnerKey), role: 8, category: storagebudget.Control, persistent: native, relative: string(binding.Default.Next), workspaces: append([]storagebudget.ID{}, row.References...), scope: &binding}
	if err = walk.current(); err != nil {
		return nil, err
	}
	x.roots = append(x.roots, source)
	x.openedRoots = append(x.openedRoots, walk.base)
	for _, edge := range walk.edges {
		x.openedRoots = append(x.openedRoots, edge.root)
	}
	accepted = true
	return source, source.current(ctx, x.construction)
}
func (x *storageOriginalSources) historyAncestorSelected(ctx context.Context, binding storageEnrollmentPeakSource) bool {
	d := binding.Default
	if x == nil || x.construction == nil || d == nil || d.Workflow != 2 || d.Branch != storageCreationPersonal || !storageEnrollmentDefaultSourceNamespace(binding) {
		return false
	}
	s := x.construction.server
	if s.containedDeskData() || s.cfg.DeskConfigDir != "" || len(d.SelectedPath) > 4096 {
		return false
	}
	selected := s.defaultChatDataPath()
	ancestor := string(d.SelectedPath)
	remainder, err := filepath.Rel(ancestor, selected)
	if err != nil || !filepath.IsLocal(remainder) || remainder == "." {
		return false
	}
	next := strings.SplitN(remainder, string(filepath.Separator), 2)[0]
	if next != string(d.Next) {
		return false
	}
	native, err := storageOwnerPersistentRoot(ctx, x.construction.original.config)
	if err != nil || native != d.Settings {
		return false
	}
	for _, selection := range x.selections {
		if selection.settings == x.construction.original.config && selection.location.Path == selected && selection.location.Previous == "" {
			return true
		}
	}
	return false
}
func (s *storageOriginalSourceRoot) historyAncestorCurrent(ctx context.Context, c *storageBudgetConstruction) error {
	if s == nil || s.role != 8 || s.scope == nil || s.scope.Default == nil || s.scope.Default.Workflow != 2 || s.parent != nil || s.path != string(s.scope.Default.SelectedPath) || s.relative != string(s.scope.Default.Next) || !c.sources.historyAncestorSelected(ctx, *s.scope) {
		return storagebudget.ErrAuthority
	}
	if err := c.originCurrent(ctx, false); err != nil {
		return err
	}
	if err := c.sources.currentProjects(ctx); err != nil {
		return err
	}
	for _, edge := range s.witnesses {
		if err := edge.current(); err != nil {
			return err
		}
	}
	native, err := storageOwnerPersistentRoot(ctx, s.root)
	if err != nil || native != s.persistent || native != s.scope.Default.Selected {
		return errors.Join(errStorageCustody, err)
	}
	return nil
}
func (w *storageCensusWalk) historyAncestorScope(r *storageRootBinding, root *os.Root, count *int, usage *[storagebudget.Categories]storagebudget.Vector) (err error) {
	if r == nil || r.source == nil || r.source.scope == nil || r.source.scope.Default == nil || root != r.root || r.source.role != 8 || r.restrictedNamespace != string(r.source.scope.Default.Next) {
		return storagebudget.ErrAuthority
	}
	if err = r.current(w.ctx); err != nil {
		return err
	}
	d := r.source.scope.Default
	if d.OwnSelf == 1 {
		info, e := root.Stat(".")
		if e != nil || !storagePrivate(info, true) {
			return errors.Join(errStorageCustody, e)
		}
		id, e := storageFileIdentity(info)
		if e != nil {
			return e
		}
		if w.directories[id] {
			return storagebudget.ErrUnknown
		}
		w.directories[id] = true
		if e = w.add(r, "", id, 0, true, usage); e != nil {
			return e
		}
		w.observation.observe(r, "", info, info)
	}
	info, err := root.Lstat(r.restrictedNamespace)
	if errors.Is(err, os.ErrNotExist) {
		return r.current(w.ctx)
	}
	if err != nil {
		return err
	}
	id, err := storageFileIdentity(info)
	if err != nil {
		return err
	}
	if child := w.roots[id]; child != nil && child != r {
		expected := filepath.Join(r.source.path, r.restrictedNamespace)
		if child.source == nil || child.source.path != expected || (child.source.role != 8 && child.source.role != 4) || !storageTransferNamespaceBinding(r) && !reflect.DeepEqual(child.references, r.references) {
			return errStorageCustody
		}
		if child.source.role == 8 && (child.source.scope == nil || child.source.scope.Default == nil || child.source.scope.Default.OwnSelf != 1) {
			return errStorageCustody
		}
		return errors.Join(child.current(w.ctx), r.current(w.ctx))
	}
	child, err := storagePinChild(root, r.restrictedNamespace, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, child.current(), child.root.Close(), r.current(w.ctx)) }()
	if storageTransferNamespaceBinding(r) {
		return w.transferDirectory(r, child, r.restrictedNamespace, count, usage)
	}
	return w.directory(r, child.root, []storageNamedRoot{child}, r.restrictedNamespace, 1, count, usage)
}
