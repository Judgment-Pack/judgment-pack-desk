package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageOriginalSourceRoot struct {
	root           *os.Root
	witnesses      []storageNamedRoot
	path, key      string
	role, category uint8
	persistent     storageOwnerPersistentObject
	marker         *storageNamedFile
	parent         *storageOriginalSourceRoot
	relative       string
	workspaces     []storagebudget.ID
	scope          *storageEnrollmentPeakSource
}
type storageOriginalProject struct {
	entry deskHomeEntry
	named storageNamedRoot
}
type storageOriginalSources struct {
	construction      *storageBudgetConstruction
	homeNamed         *storageNamedRoot
	projects          map[string]storageOriginalProject
	roots             []*storageOriginalSourceRoot
	objects           []storageOwnerObject
	objectParents     []storageOwnerPersistentObject
	configObject      uint32
	workspaces        []storageOwnerWorkspace
	owner             storageOwnerSource
	controls          []storageOwnerControl
	openedRoots       []*os.Root
	openedFiles       []*os.File
	selections        []storageOriginalHistorySelection
	selectedProviders map[string][]storagebudget.ID
}
type storageOriginalHistorySelection struct {
	settings *os.Root
	path     string
	location dataLocation
	explicit bool
	pending  *storageCreationLive
	// Original config-selected legacy pointer observation. This owns no Close:
	// openedFiles retains the sole file lifetime, just like original markers.
	pointer       *storageNamedFile
	pointerNative storageOwnerPersistentObject
}

func (c *storageBudgetConstruction) inspectOriginalSources(ctx context.Context, installation storagebudget.ID) (out *storageOriginalSources, err error) {
	if err = c.current(ctx, true); err != nil {
		return nil, err
	}
	x := &storageOriginalSources{construction: c, projects: map[string]storageOriginalProject{}, selectedProviders: map[string][]storagebudget.ID{}, owner: storageOwnerSource{Kind: 1, ConfigPath: []byte(c.server.configDir), HomeOwner: c.server.installationOwner(), HomePath: []byte{}, RegistryControl: storageOwnerNoControl, Entries: []storageOwnerHomeRelation{}}, workspaces: []storageOwnerWorkspace{}, controls: []storageOwnerControl{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, x.Close())
		}
	}()
	config, err := x.addRoot(ctx, c.original.config, nil, c.server.configDir, c.server.installationOwner(), 1, storagebudget.Control, nil, "")
	if err != nil {
		return nil, err
	}
	if _, err = x.addRoot(ctx, c.anchor, nil, filepath.Join(c.server.configDir, storageAnchorName), c.server.installationOwner(), 2, storagebudget.Control, config, storageAnchorName); err != nil {
		return nil, err
	}
	// The current project has already been adopted by New. Other Desk projects
	// come only from the freshly re-read original home registry under home EX.
	if c.server.home != nil {
		h, readErr := readDeskHome(c.server.configDir)
		if readErr != nil || !reflect.DeepEqual(h, c.server.home) {
			return nil, errors.Join(errStorageCustody, readErr)
		}
		// Anchor the complete original home name at the filesystem root. A held
		// pathname parent alone would miss replacement of an ancestor's name.
		if !filepath.IsAbs(h.Path) || filepath.VolumeName(h.Path) != "" {
			return nil, storagebudget.ErrUnsupported
		}
		base, readErr := os.OpenRoot(string(filepath.Separator))
		if readErr != nil {
			return nil, readErr
		}
		x.openedRoots = append(x.openedRoots, base)
		name, readErr := filepath.Rel(string(filepath.Separator), h.Path)
		if readErr != nil || name != "." && !filepath.IsLocal(name) {
			return nil, errors.Join(errStorageCustody, readErr)
		}
		homeNamed, readErr := storagePinChild(base, name, false)
		if readErr != nil {
			return nil, readErr
		}
		x.homeNamed = &homeNamed
		homeRoot := homeNamed.root
		x.openedRoots = append(x.openedRoots, homeRoot)
		hp, readErr := storageOwnerPersistentRoot(ctx, homeRoot)
		if readErr != nil {
			return nil, readErr
		}
		x.owner.Kind = 2
		x.owner.HomePath = []byte(h.Path)
		x.owner.HomeObject = hp
		x.objects = append(x.objects, storageOwnerObject{Role: 3, Source: 2, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: hp, OwnerClass: 1, ModeClass: 1})
		x.objectParents = append(x.objectParents, storageOwnerPersistentObject{})
		for _, entry := range h.Registry.Desks {
			kind := uint8(1)
			if entry.Kind == "desk" {
				kind = 2
			}
			x.owner.Entries = append(x.owner.Entries, storageOwnerHomeRelation{DeskID: entry.ID, Folder: []byte(entry.Folder), Kind: kind, HistoryIdentity: []byte(entry.HistoryIdentity), StorageOwner: entry.StorageOwner, RunnerWorkspace: entry.RunnerWorkspace})
			projectNamed, readErr := storagePinChild(homeRoot, entry.Folder, false)
			if readErr != nil {
				return nil, readErr
			}
			project := projectNamed.root
			x.openedRoots = append(x.openedRoots, project)
			if _, duplicate := x.projects[entry.ID]; duplicate {
				return nil, errStorageCustody
			}
			x.projects[entry.ID] = storageOriginalProject{entry: entry, named: projectNamed}
			if filepath.Join(h.Path, entry.Folder) == c.server.projectDir {
				a, e := project.Stat(".")
				b, be := c.server.root.Stat(".")
				if e != nil || be != nil || !os.SameFile(a, b) {
					return nil, errors.Join(errStorageCustody, e, be)
				}
			}
			if readErr = x.inspectProject(ctx, project, filepath.Join(h.Path, entry.Folder), entry.ID, entry.RunnerWorkspace, entry.HistoryIdentity, entry.StorageOwner, installation); readErr != nil {
				return nil, readErr
			}
		}
		sort.Slice(x.owner.Entries, func(i, j int) bool { return x.owner.Entries[i].DeskID < x.owner.Entries[j].DeskID })
	} else {
		key, readErr := c.inspectOriginalStartupKey(ctx)
		if readErr != nil {
			return nil, readErr
		}
		history, readErr := c.originalStorageHistory(ctx)
		if readErr != nil {
			return nil, readErr
		}
		if err = x.inspectProject(ctx, c.server.root, c.server.projectDir, c.server.cfg.deskID, key, history, c.server.storageOwner(), installation); err != nil {
			return nil, err
		}
	}
	// The original config selection owns legacy/default/current/Previous data.
	// Discovery reads the original pointer; it never invokes openChatData's
	// create/claim/pointer-write path and never treats unreadability as absence.
	for _, w := range x.workspaces {
		config.workspaces = append(config.workspaces, w.Definition.ID)
	}
	if !c.server.containedDeskData() {
		if err = x.inspectHistory(ctx, c.original.config, config, c.server.configDir, c.server.storageOwner(), c.server.defaultChatDataPath(), installation); err != nil {
			return nil, err
		}
	}
	for id, refs := range x.selectedProviders {
		if c.providers == nil || c.providers.byID[id] == nil {
			return nil, storagebudget.ErrAuthority
		}
		registration := c.providers.byID[id]
		if err = registration.current(); err != nil {
			return nil, err
		}
		pins := c.server.storageStartupProviders
		if pins == nil || pins.byID[id] == nil {
			return nil, storagebudget.ErrAuthority
		}
		if err = pins.byID[id].check(ctx); err != nil {
			return nil, err
		}
		p := registration.provider
		locator, valid := storageInjectedProviderSourceLocator(p.root.Name())
		if !valid {
			return nil, storagebudget.ErrUnsupported
		}
		r, readErr := x.addRoot(ctx, p.root, nil, locator, p.id, 5, storagebudget.Retained, nil, "")
		if readErr != nil {
			return nil, readErr
		}
		r.workspaces = storageUniqueIDs(refs)
		b, n, readErr := storageOwnerReadFixed(p.root, "d5-provider.json", int64(len(p.identity)))
		if readErr != nil {
			return nil, readErr
		}
		x.openedFiles = append(x.openedFiles, n.file)
		n.immutable = append([]byte(nil), b...)
		if !bytes.Equal(b, p.identity) {
			return nil, errStorageCustody
		}
		r.marker = &n
	}
	if err = c.current(ctx, true); err != nil {
		return nil, err
	}
	return x, nil
}

// The provider constructor owns root.OpenRoot("."), whose display name adds
// exactly one "/.". Remove that suffix only for source comparison; the original
// held root, registration and native identity remain the source of custody.
func storageInjectedProviderSourceLocator(raw string) (string, bool) {
	if len(raw) > 4098 {
		return "", false
	}
	locator := raw
	if len(locator) >= 2 && locator[len(locator)-2:] == "/." {
		locator = locator[:len(locator)-2]
		// Trimming "//." must not hide its duplicate root separator.
		if locator == "/" {
			return "", false
		}
		if locator == "" {
			locator = "/"
		}
	}
	if !storageOwnerAbsolute([]byte(locator)) {
		return "", false
	}
	return locator, true
}

// Read-only identity inspection precedes any default startupName decision.
// A retained key is used exactly; copied/moved recovery remains its own owner.
func (s *Server) inspectStorageStartupKey() (key string, err error) {
	if s.cfg.deskID != "" {
		return s.cfg.deskID, nil
	}
	private, err := s.openIdentityFolder()
	if errors.Is(err, os.ErrNotExist) {
		return digestOf([]byte(s.projectDir)), nil
	}
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, private.Close()) }()
	r, _, found, err := readIdentity(private)
	if err != nil {
		return "", err
	}
	if found {
		return r.ID, nil
	}
	return digestOf([]byte(s.projectDir)), nil
}

func (x *storageOriginalSources) addRoot(ctx context.Context, root *os.Root, w []storageNamedRoot, path, key string, role, category uint8, parent *storageOriginalSourceRoot, relative string) (*storageOriginalSourceRoot, error) {
	if len(x.roots) >= 4096 {
		return nil, storagebudget.ErrBounds
	}
	p, err := storageOwnerPersistentRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	for _, old := range x.roots {
		if old.persistent == p {
			if old.path != path || old.key != key || old.role != role {
				return nil, storagebudget.ErrAuthority
			}
			return old, nil
		}
	}
	r := &storageOriginalSourceRoot{root: root, witnesses: w, path: path, key: key, role: role, category: category, persistent: p, parent: parent, relative: relative}
	x.roots = append(x.roots, r)
	return r, nil
}

func (x *storageOriginalSources) inspectProject(ctx context.Context, project *os.Root, path, deskID, key, history, owner string, installation storagebudget.ID) error {
	if key == "" || !storageOwnerHistoryIdentity([]byte(history)) {
		return storagebudget.ErrAuthority
	}
	id := storageDerivedID("desk-storage-workspace-v2", installation, []byte(key))
	if construction := x.construction.server.cfg.retainedArtifactConstruction; construction != nil {
		info, err := project.Stat(".")
		if err != nil {
			return err
		}
		provider, found := construction.projects[identityKey(info)]
		if !found {
			return storagebudget.ErrAuthority
		}
		if provider != "" {
			x.selectedProviders[provider] = append(x.selectedProviders[provider], id)
		}
	}
	w := storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: id, Generation: 1, Limits: storageHostInitialPolicy().Workspace}, OwnerKind: 3, OwnerKey: []byte(key), HistoryIdentity: []byte(history)}
	// Rediscovery retains the immutable admission baseline, including a deliberate
	// value equal to an earlier default. Only a genuinely new ID captures the
	// currently admitted default; equality is never used to infer inheritance.
	c := x.construction
	if c.ref != nil && c.ref.cell.ownerRecord != nil {
		c.ref.cell.mu.Lock()
		original := c.ref.cell.ownerCatalog
		c.ref.cell.mu.Unlock()
		found := false
		for _, old := range original.Workspaces {
			if old.Definition.ID != id {
				continue
			}
			if found || old.OwnerKind != w.OwnerKind || !bytes.Equal(old.OwnerKey, w.OwnerKey) || !bytes.Equal(old.HistoryIdentity, w.HistoryIdentity) {
				return storagebudget.ErrAuthority
			}
			w.Definition = old.Definition
			found = true
		}
		if !found {
			view, err := c.ref.cell.q.InspectHost(ctx)
			if err != nil {
				return err
			}
			w.Definition.Limits = view.Configuration.Policy.Workspace
		}
	}
	for _, old := range x.workspaces {
		if old.Definition.ID == id {
			if !reflect.DeepEqual(old, w) {
				return storagebudget.ErrAuthority
			}
			return nil
		}
	}
	if len(x.workspaces) >= 1024 {
		return storagebudget.ErrBounds
	}
	x.workspaces = append(x.workspaces, w)
	n, err := storagePinChild(project, ".desk-private", true)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// storagePinChild deliberately returns a custody refusal for unsafe/missing
	// names; prove absence on the actual adopted parent before accepting it.
	if err != nil {
		if _, le := project.Lstat(".desk-private"); errors.Is(le, os.ErrNotExist) {
			return nil
		}
		return err
	}
	x.openedRoots = append(x.openedRoots, n.root)
	role := uint8(6)
	rootKey := key
	if deskID != "" {
		role = 3
		rootKey = deskID
	}
	r, err := x.addRoot(ctx, n.root, []storageNamedRoot{n}, filepath.Join(path, ".desk-private"), rootKey, role, storagebudget.Control, nil, "")
	if err != nil {
		return err
	}
	r.workspaces = []storagebudget.ID{id}
	if deskID != "" || x.construction.server.home != nil {
		if err = x.inspectHistory(ctx, n.root, r, filepath.Join(path, ".desk-private"), owner, filepath.Join(path, ".desk-private", "data"), installation); err != nil {
			return err
		}
	}
	retained, err := storagePinChild(n.root, "retained-artifacts", true)
	if err != nil {
		if _, le := n.root.Lstat("retained-artifacts"); errors.Is(le, os.ErrNotExist) {
			return nil
		}
		return err
	}
	x.openedRoots = append(x.openedRoots, retained.root)
	child, err := x.addRoot(ctx, retained.root, []storageNamedRoot{n, retained}, filepath.Join(path, ".desk-private", "retained-artifacts"), key, 6, storagebudget.Retained, r, "retained-artifacts")
	if err == nil {
		child.workspaces = []storagebudget.ID{id}
	}
	return err
}

func (x *storageOriginalSources) inspectHistory(ctx context.Context, settings *os.Root, parent *storageOriginalSourceRoot, path, owner, defaultPath string, installation storagebudget.ID) error {
	b, err := readPrivateData(settings, dataLocationName, 16384)
	location := dataLocation{Version: 1, Path: defaultPath}
	explicit := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if explicit {
		if err = decodeDataJSON(b, &location); err != nil || location.Version != 1 {
			return errors.Join(errStorageCustody, err)
		}
		if parent.role == 3 || parent.role == 6 {
			if !filepath.IsLocal(location.Path) {
				return errStorageCustody
			}
			location.Path = filepath.Join(path, location.Path)
			if location.Previous != "" {
				if !filepath.IsLocal(location.Previous) {
					return errStorageCustody
				}
				location.Previous = filepath.Join(path, location.Previous)
			}
		}
	}
	x.selections = append(x.selections, storageOriginalHistorySelection{settings: settings, path: path, location: location, explicit: explicit})
	control := -1 // one pointer on this original settings object, not one per owner
	for _, selected := range []string{location.Path, location.Previous} {
		if selected == "" || selected == x.construction.server.configDir {
			continue
		}
		if !filepath.IsAbs(selected) || filepath.Clean(selected) != selected {
			return errStorageCustody
		}
		root, readErr := openPrivateDataRoot(selected, false)
		if readErr != nil {
			if !explicit && errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return readErr
		}
		x.openedRoots = append(x.openedRoots, root)
		marker, readErr := readPrivateData(root, dataMarkerName, 4096)
		if readErr != nil {
			return readErr
		}
		var m dataMarker
		if readErr = decodeDataJSON(marker, &m); readErr != nil || m.Version != 1 || m.Owner != owner {
			return errors.Join(errStorageCustody, readErr)
		}
		var relative string
		var p *storageOriginalSourceRoot
		if rel, re := filepath.Rel(path, selected); re == nil && filepath.IsLocal(rel) {
			relative = rel
			p = parent
		}
		r, readErr := x.addRoot(ctx, root, nil, selected, owner, 4, storagebudget.History, p, relative)
		if readErr != nil {
			return readErr
		}
		r.workspaces = append([]storagebudget.ID(nil), parent.workspaces...)
		n, readErr := storageOpenFile(root, dataMarkerName, int64(len(marker)))
		if readErr != nil {
			return readErr
		}
		n.immutable = append([]byte(nil), marker...)
		r.marker = &n
		x.openedFiles = append(x.openedFiles, n.file)
		if explicit {
			if control < 0 {
				x.controls = append(x.controls, storageOwnerControl{Role: 2, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte(owner), MaxBytes: 16384, Location: storageOwnerLocation{Version: 1, Path: []byte(location.Path), Previous: []byte(location.Previous)}})
				control = len(x.controls) - 1
			}
			ctrl := &x.controls[control]
			if selected == location.Path {
				ctrl.Location.Selected = r.persistent
			} else {
				ctrl.Location.PreviousSelected = r.persistent
			}
		}
	}
	return nil
}

// A shared storage owner does not identify a pointer. Its exact selected path
// and native identify this branch; the caller separately joins its original
// settings parent. An ambiguous current/previous tuple names neither branch.
func storageHistoryControlSelection(control storageOwnerControl, source *storageOriginalSourceRoot) byte {
	if source == nil || source.role != 4 || control.Role != 2 || string(control.OwnerKey) != source.key {
		return 0
	}
	current := string(control.Location.Path) == source.path && control.Location.Selected == source.persistent
	previous := len(control.Location.Previous) > 0 && string(control.Location.Previous) == source.path && control.Location.PreviousSelected == source.persistent
	if current == previous {
		return 0
	}
	if current {
		return 1
	}
	return 2
}

func (x *storageOriginalSources) exactRoot(ctx context.Context, c storageOwnerCatalogV2, r storageOwnerRoot) (*storageOriginalSourceRoot, error) {
	if r.Role == 8 && r.Source.Kind == 9 {
		return x.exactHistoryAncestorScope(ctx, c, r)
	}
	if r.Role == 7 && r.Source.Kind == 7 {
		return x.exactProjectScope(ctx, c, r)
	}
	if r.Source.Kind == 8 {
		return x.exactCapturedPrivateChild(ctx, c, r)
	}
	for _, s := range x.roots {
		if s.key == string(r.Source.OwnerKey) && s.role == r.Role && s.persistent == c.Objects[r.DirectoryObject].Persistent {
			if s.path != string(r.Source.Path) {
				matched, err := x.movePrivateSourceCurrent(ctx, c, r, s)
				if err != nil {
					return nil, err
				}
				if !matched {
					continue
				}
			}
			for _, n := range s.witnesses {
				if err := n.current(); err != nil {
					return nil, err
				}
			}
			p, err := storageOwnerPersistentRoot(ctx, s.root)
			if err != nil || p != s.persistent {
				return nil, errors.Join(errStorageCustody, err)
			}
			return s, nil
		}
	}
	return x.exactCapturedTransferRoot(ctx, c, r)
}

// Retain every registered project edge, even when its private data is absent.
// This is live original-name custody, never authority from a saved selector.
func (x *storageOriginalSources) currentProjects(ctx context.Context) error {
	if ctx == nil || x == nil || x.construction == nil || x.construction.self != x.construction {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if x.owner.Kind != 2 {
		return nil
	}
	if x.homeNamed == nil || x.construction.server.home == nil || string(x.owner.HomePath) != x.construction.server.home.Path || x.owner.HomeOwner != x.construction.server.home.Registry.Owner || len(x.projects) != len(x.construction.server.home.Registry.Desks) {
		return errStorageCustody
	}
	b, err := readPrivateData(x.construction.original.config, deskHomeRegistryName, 1048576)
	if err != nil {
		return err
	}
	registry, err := decodeHomeRegistry(b)
	if err != nil || !reflect.DeepEqual(registry, x.construction.server.home.Registry) {
		return errors.Join(errStorageCustody, err)
	}
	if err := x.homeNamed.current(); err != nil {
		return err
	}
	for _, entry := range x.construction.server.home.Registry.Desks {
		project, ok := x.projects[entry.ID]
		if !ok || project.entry != entry || project.named.parent != x.homeNamed.root {
			return errStorageCustody
		}
		if err := project.named.current(); err != nil {
			return err
		}
	}
	return x.homeNamed.current()
}

func (x *storageOriginalSources) current(ctx context.Context) error {
	if x != nil && x.construction != nil {
		if op := x.construction.originalTransferOperation(); op != nil {
			return op.originalReadCurrent(ctx)
		}
	}
	if err := x.currentProjects(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, s := range x.roots {
		if s.scope != nil {
			if err := s.current(ctx, x.construction); err != nil {
				return err
			}
		}
		for _, n := range s.witnesses {
			if err := n.current(); err != nil {
				return err
			}
		}
		if s.marker != nil {
			if err := s.marker.current(); err != nil {
				return err
			}
		}
		if s.role == 4 && len(s.witnesses) == 0 {
			// The source path comes from the actual original owner's selection,
			// never from a catalog resolver. Reopening here checks its current name.
			n, err := openPrivateDataRoot(s.path, false)
			if err != nil {
				return err
			}
			a, e := s.root.Stat(".")
			b, be := n.Stat(".")
			ce := n.Close()
			if e != nil || be != nil || ce != nil || !os.SameFile(a, b) {
				return errors.Join(errStorageCustody, e, be, ce)
			}
		}
	}
	for _, s := range x.selections {
		if s.pending != nil {
			if err := s.pending.pendingPointerCurrent(ctx, s); err != nil {
				return err
			}
			continue
		}
		if s.pointer != nil {
			if err := s.pointer.current(); err != nil {
				return err
			}
			native, err := storageOwnerPersistentFile(ctx, *s.pointer)
			if err != nil || native != s.pointerNative {
				return errors.Join(errStorageCustody, err)
			}
		}
		b, err := readPrivateData(s.settings, dataLocationName, 16384)
		if !s.explicit {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return errors.Join(errStorageCustody, err)
		}
		if err != nil {
			return err
		}
		var l dataLocation
		if err = decodeDataJSON(b, &l); err != nil {
			return err
		}
		if !filepath.IsAbs(l.Path) {
			if !filepath.IsLocal(l.Path) {
				return errStorageCustody
			}
			l.Path = filepath.Join(s.path, l.Path)
			if l.Previous != "" {
				if !filepath.IsLocal(l.Previous) {
					return errStorageCustody
				}
				l.Previous = filepath.Join(s.path, l.Previous)
			}
		}
		if l != s.location {
			return errStorageOwnerTransitionRequired
		}
	}
	return nil
}

func (s *storageOriginalSourceRoot) current(ctx context.Context, c *storageBudgetConstruction) error {
	if c == nil || c.self != c || c.sources == nil {
		return storagebudget.ErrAuthority
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.scope != nil {
		if s.scope.Default != nil {
			if s.scope.Default.Workflow == 4 || s.scope.Default.Workflow == 6 {
				return s.transferScopeCurrent(ctx, c)
			}
			return s.historyAncestorCurrent(ctx, c)
		}
		if s.role != 7 || s.relative != ".desk-private" || s.parent != nil || !bytes.Equal(s.scope.Namespace, []byte(".desk-private")) {
			return storagebudget.ErrAuthority
		}
		if err := c.originCurrent(ctx, false); err != nil {
			return err
		}
		if err := c.sources.currentProjects(ctx); err != nil {
			return err
		}
		for _, witness := range s.witnesses {
			if err := witness.current(); err != nil {
				return err
			}
		}
		native, err := storageOwnerPersistentRoot(ctx, s.root)
		if err != nil || native != s.scope.Project || native != s.persistent {
			return errors.Join(errStorageCustody, err)
		}
		return nil
	}
	if s.parent != nil {
		if err := s.parent.current(ctx, c); err != nil {
			return err
		}
	}
	for _, n := range s.witnesses {
		if err := n.current(); err != nil {
			return err
		}
	}
	if s.marker != nil {
		if err := s.marker.current(); err != nil {
			return err
		}
	}
	if s.role == 4 && len(s.witnesses) == 0 {
		n, err := openPrivateDataRoot(s.path, false)
		if err != nil {
			return err
		}
		a, e := s.root.Stat(".")
		b, be := n.Stat(".")
		ce := n.Close()
		if e != nil || be != nil || ce != nil || !os.SameFile(a, b) {
			return errors.Join(errStorageCustody, e, be, ce)
		}
	}
	if s.role == 5 {
		if c.providers == nil || c.providers.byID[s.key] == nil || c.providers.byID[s.key].root != s.root {
			return storagebudget.ErrAuthority
		}
		pins := c.server.storageStartupProviders
		if pins == nil || pins.byID[s.key] == nil {
			return storagebudget.ErrAuthority
		}
		return pins.byID[s.key].check(ctx)
	}
	originalTransfer := c.originalTransferOperation()
	for _, selection := range c.sources.selections {
		if selection.settings != s.root && selection.location.Path != s.path && selection.location.Previous != s.path {
			continue
		}
		// Only this original operation may observe its own published pointer
		// before the owning successor installs the new selection dictionary.
		// All physical/scope checks above and every other selection stay strict.
		if originalTransfer != nil && originalTransfer.settings != nil && selection.settings == originalTransfer.settings {
			if err := originalTransfer.originalTransferSelectionCurrent(ctx, selection); err != nil {
				return err
			}
			continue
		}
		if selection.pending != nil {
			if err := selection.pending.pendingPointerCurrent(ctx, selection); err != nil {
				return err
			}
			continue
		}
		b, err := readPrivateData(selection.settings, dataLocationName, 16384)
		if !selection.explicit {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return errors.Join(errStorageOwnerTransitionRequired, err)
		}
		if err != nil {
			return err
		}
		var l dataLocation
		if err = decodeDataJSON(b, &l); err != nil {
			return err
		}
		if !filepath.IsAbs(l.Path) {
			if !filepath.IsLocal(l.Path) {
				return errStorageCustody
			}
			l.Path = filepath.Join(selection.path, l.Path)
			if l.Previous != "" {
				if !filepath.IsLocal(l.Previous) {
					return errStorageCustody
				}
				l.Previous = filepath.Join(selection.path, l.Previous)
			}
		}
		if l != selection.location {
			return errStorageOwnerTransitionRequired
		}
	}
	return nil
}
func (x *storageOriginalSources) Close() (err error) {
	for i := len(x.openedFiles) - 1; i >= 0; i-- {
		err = errors.Join(err, x.openedFiles[i].Close())
	}
	x.openedFiles = nil
	for i := len(x.openedRoots) - 1; i >= 0; i-- {
		err = errors.Join(err, x.openedRoots[i].Close())
	}
	x.openedRoots = nil
	return err
}

// V3 deliberately cannot represent an orphan config-selected location control.
// Peak recovery calls this only when its exact persisted catalog or captured
// Workflow3 record requires that relation. The selection is still discovered
// from the original pointer; no catalog/path/body establishes creation custody.
// Keep config as the selected original root: no History directory or marker.
func (x *storageOriginalSources) observeLegacyHistoryControl(ctx context.Context) error {
	if x == nil || x.construction == nil || x.construction.ref.cell.ownerRecord == nil || int(x.configObject) >= len(x.objects) {
		return storagebudget.ErrAuthority
	}
	c := x.construction
	if err := x.current(ctx); err != nil {
		return err
	}
	config, err := storageOwnerPersistentRoot(ctx, c.original.config)
	if err != nil || config != c.ref.cell.ownerRecord.Config || x.objects[x.configObject].Role != 1 || x.objects[x.configObject].Persistent != config {
		return errors.Join(errStorageCustody, err)
	}
	var selection *storageOriginalHistorySelection
	for i := range x.selections {
		v := &x.selections[i]
		if v.settings == c.original.config && v.explicit && v.location.Path == c.server.configDir && v.location.Previous == "" {
			if selection != nil {
				return errStorageCustody
			}
			selection = v
		}
	}
	if selection == nil || c.server.containedDeskData() {
		return errStorageCustody
	}
	if selection.pointer == nil {
		raw, err := readPrivateData(selection.settings, dataLocationName, 16384)
		if err != nil {
			return err
		}
		var actual dataLocation
		if err = decodeDataJSON(raw, &actual); err != nil || actual != selection.location {
			return errors.Join(errStorageCustody, err)
		}
		named, err := storageOpenFile(selection.settings, dataLocationName, int64(len(raw)))
		if err != nil {
			return err
		}
		named.immutable = append([]byte{}, raw...)
		x.openedFiles = append(x.openedFiles, named.file)
		selection.pointer = &named
		selection.pointerNative, err = storageOwnerPersistentFile(ctx, named)
		if err != nil || !selection.pointerNative.valid(2) {
			return errors.Join(errStorageCustody, err)
		}
		if err = named.current(); err != nil {
			return err
		}
	}
	observed := storageOwnerControl{Role: 2, ParentObject: x.configObject, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: []byte(c.server.storageOwner()), MaxBytes: 16384, Location: storageOwnerLocation{Version: 1, Path: []byte(selection.location.Path), Previous: []byte{}, Selected: config}}
	current := storageOwnerCatalogV2{OriginalOwner: x.owner, Objects: x.objects, OwnerControls: append([]storageOwnerControl{}, x.controls...)}
	observed.RelationDigest, err = current.controlDigest(observed)
	if err != nil {
		return err
	}
	for _, old := range current.OwnerControls {
		if int(old.ParentObject) >= len(current.Objects) {
			return storagebudget.ErrBounds
		}
		if current.Objects[old.ParentObject].Persistent == config && bytes.Equal(old.Name, observed.Name) {
			if !reflect.DeepEqual(old, observed) {
				return errStorageCustody
			}
			return x.current(ctx)
		}
	}
	current.OwnerControls = append(current.OwnerControls, observed)
	if err = storageEnrollmentSortControls(&current); err != nil {
		return err
	}
	x.owner = current.OriginalOwner
	x.controls = current.OwnerControls
	return x.current(ctx)
}
func (x *storageOriginalSources) observeCatalogLegacyHistoryControl(ctx context.Context, catalog storageOwnerCatalogV2) error {
	if x == nil || x.construction == nil {
		return storagebudget.ErrAuthority
	}
	config := x.construction.ref.cell.ownerRecord.Config
	for _, control := range catalog.OwnerControls {
		if control.Role == 2 && int(control.ParentObject) < len(catalog.Objects) && catalog.Objects[control.ParentObject].Persistent == config && control.Location.Selected == config && string(control.Location.Path) == x.construction.server.configDir && len(control.Location.Previous) == 0 {
			return x.observeLegacyHistoryControl(ctx)
		}
	}
	return nil
}
