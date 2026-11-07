package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// A finite read-only walk of the platform-selected default path. Every
// component is opened and retained separately, so an intermediate symlink or
// exchanged named edge cannot become an ancestry witness. No contents of an
// ordinary host ancestor are enumerated.
type storageHistoryAncestry struct {
	base    *os.Root
	edges   []storageNamedRoot
	path    string
	next    string
	missing bool
}

func (w *storageHistoryAncestry) root() *os.Root {
	if len(w.edges) == 0 {
		return w.base
	}
	return w.edges[len(w.edges)-1].root
}
func (w *storageHistoryAncestry) current() error {
	if w == nil || w.base == nil {
		return storagebudget.ErrAuthority
	}
	for _, edge := range w.edges {
		if err := edge.current(); err != nil {
			return err
		}
	}
	if w.missing {
		if _, err := w.root().Lstat(w.next); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
	}
	return nil
}
func (w *storageHistoryAncestry) Close() (err error) {
	if w == nil {
		return nil
	}
	err = w.current()
	for i := len(w.edges) - 1; i >= 0; i-- {
		err = errors.Join(err, w.edges[i].root.Close())
	}
	if w.base != nil {
		err = errors.Join(err, w.base.Close())
		w.base = nil
	}
	return err
}
func storageWalkHistoryAncestry(selected string) (_ *storageHistoryAncestry, err error) {
	if !storageOwnerAbsolute([]byte(selected)) || filepath.Clean(selected) != selected || filepath.Base(selected) != "history" {
		return nil, storagebudget.ErrBounds
	}
	base, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	w := &storageHistoryAncestry{base: base, path: string(filepath.Separator), next: "history"}
	accepted := false
	defer func() {
		if !accepted {
			err = errors.Join(err, w.Close())
		}
	}()
	relative, err := filepath.Rel(string(filepath.Separator), filepath.Dir(selected))
	if err != nil || !filepath.IsLocal(relative) {
		return nil, errors.Join(errStorageCustody, err)
	}
	if relative != "." {
		for _, name := range strings.Split(relative, string(filepath.Separator)) {
			if !storageOwnerCatalogNameComponent([]byte(name)) {
				return nil, storagebudget.ErrBounds
			}
			if _, err = w.root().Lstat(name); errors.Is(err, os.ErrNotExist) {
				w.next = name
				w.missing = true
				accepted = true
				return w, w.current()
			} else if err != nil {
				return nil, err
			}
			edge, e := storagePinChild(w.root(), name, false)
			if e != nil {
				return nil, e
			}
			w.edges = append(w.edges, edge)
			w.path = filepath.Join(w.path, name)
		}
	}
	accepted = true
	return w, w.current()
}

// Each successful iteration enrolls at most one actual mkdir. Path length and
// the catalog/packet bounds already make the chain finite; MaxComponents is
// still the complete per-operation accounting limit, not a depth allowance.
func (c *storageBudgetConstruction) enrollDefaultHistoryAncestors(ctx context.Context, s *Server) error {
	if s != c.server || s.containedDeskData() || s.cfg.DeskConfigDir != "" {
		return storagebudget.ErrAuthority
	}
	for {
		complete, err := c.enrollNextHistoryAncestor(ctx, s)
		if err != nil {
			return err
		}
		if complete {
			return nil
		}
	}
}
func (c *storageBudgetConstruction) enrollNextHistoryAncestor(ctx context.Context, s *Server) (complete bool, err error) {
	p, err := c.beginEnrollmentEpoch(ctx)
	if err != nil {
		return false, err
	}
	defer func() { err = p.finish(err) }()
	if err = p.acquire(ctx); err != nil {
		return false, err
	}
	if err = p.captureExistingOriginal(ctx); err != nil {
		return false, err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return false, err
	}
	settings, selection, err := c.historySettings(ctx, s)
	if err != nil {
		return false, err
	}
	if selection.explicit {
		return true, nil
	}
	// Original legacy history selects config itself and needs no host ancestry.
	entries, err := storageEntries(settings.root)
	if err != nil {
		return false, err
	}
	if storageDefaultLegacyNames(entries) {
		return true, nil
	}
	if err = s.validateDemandedHistoryPath(s.defaultChatDataPath()); err != nil {
		return false, err
	}
	walk, err := storageWalkHistoryAncestry(s.defaultChatDataPath())
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, walk.Close()) }()
	native, err := storageOwnerPersistentRoot(ctx, walk.root())
	if err != nil {
		return false, err
	}
	var parent *storageRootBinding
	for _, root := range p.originalRoots {
		if !root.transferInactive && root.source != nil && root.source.persistent == native {
			if parent != nil || root.source.role != 8 || root.source.scope == nil || root.source.scope.Default == nil || root.source.path != walk.path || root.source.relative != walk.next {
				return false, errStorageCustody
			}
			parent = root
		}
	}
	if parent == nil {
		catalog, source, e := p.planHistoryAncestorCatalog(ctx, s, native, walk.path, walk.next, 0, storagebudget.Digest{}, storagebudget.ID{})
		if e != nil {
			return false, e
		}
		if e = walk.current(); e != nil {
			return false, e
		}
		if e = p.enrollHistoryAncestorCatalog(ctx, catalog, source); e != nil {
			return false, e
		}
		// A new epoch obtains the installed root bindings before issuing a member.
		return !walk.missing, nil
	}
	if err = parent.current(ctx); err != nil {
		return false, err
	}
	if !walk.missing {
		return true, nil
	}
	r := storageCreationIntent{Kind: storageCreationAncestor, Branch: storageCreationPersonal, SelectedPath: filepath.Join(walk.path, walk.next), Core: storageIntentRecord{Target: walk.next, Stage: walk.next, Workflow: "desk-monotone-private-creation-v1"}}
	component, err := p.creationComponentGroup(ctx, &r.Core, parent, storagebudget.Control, storagebudget.Vector{Directories: 1}, 1)
	if err != nil {
		return false, err
	}
	r.Parents = []storageCreationParent{{Native: native, Role: 7, Component: component}}
	r.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: walk.next}}
	if err = walk.current(); err != nil {
		return false, err
	}
	// Positive absence has now been consumed by the single creator invocation;
	// final currentness checks the retained original edges, not continued absence.
	walk.missing = false
	member, err := p.issueOriginalCreationFor(ctx, s, r, nil)
	if err != nil {
		return false, err
	}
	if err = member.execute(ctx); err != nil {
		return false, err
	}
	if err = member.captureRetiredOriginal(ctx); err != nil {
		return false, err
	}
	next, err := storageHistoryNextComponent(member.record.SelectedPath, s.defaultChatDataPath())
	if err != nil {
		return false, err
	}
	catalog, source, err := p.planHistoryAncestorCatalog(ctx, s, member.record.Effects[0].Native, member.record.SelectedPath, next, 1, sha256.Sum256(member.selected.page), member.record.Tail.Bridge)
	if err != nil {
		return false, err
	}
	if err = member.retainCreatedAncestorSource(ctx, catalog, source); err != nil {
		return false, err
	}
	return false, p.enrollHistoryAncestorCatalog(ctx, catalog, source)
}
func storageHistoryNextComponent(ancestor, selected string) (string, error) {
	relative, err := filepath.Rel(ancestor, selected)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return "", errors.Join(errStorageCustody, err)
	}
	next := strings.SplitN(relative, string(filepath.Separator), 2)[0]
	if !storageOwnerCatalogNameComponent([]byte(next)) {
		return "", storagebudget.ErrBounds
	}
	return next, nil
}
func (p *storageEnrollmentEpoch) enrollHistoryAncestorCatalog(ctx context.Context, catalog storageOwnerCatalogV2, source storageEnrollmentPeakSource) error {
	fence, prefix, err := p.planSuccessorEnrollment(ctx, catalog, source)
	if err != nil {
		return err
	}
	if err = p.admitInitialEnrollment(ctx, fence, prefix); err != nil {
		return err
	}
	if err = p.persistInitialEnrollmentFences(ctx); err != nil {
		return err
	}
	return p.runSuccessorEnrollmentControls(ctx)
}

func (p *storageEnrollmentEpoch) planHistoryAncestorCatalog(ctx context.Context, s *Server, native storageOwnerPersistentObject, path, next string, own byte, page storagebudget.Digest, bridge storagebudget.ID) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	return p.planHistoryAncestorCatalogOriginal(ctx, s, native, path, next, own, page, bridge, nil)
}
func (p *storageEnrollmentEpoch) planHistoryAncestorCatalogOriginal(ctx context.Context, s *Server, native storageOwnerPersistentObject, path, next string, own byte, page storagebudget.Digest, bridge storagebudget.ID, intended *storageEnrollmentPeakSource) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	var catalog storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if p == nil || p.previousEvidence == nil || p.initialInspection == nil || s != p.c.server || !native.valid(1) || own > 1 || own == 0 && (page != (storagebudget.Digest{}) || intended == nil && bridge != (storagebudget.ID{})) || own == 1 && (page == (storagebudget.Digest{}) || bridge == (storagebudget.ID{})) {
		return catalog, source, storagebudget.ErrAuthority
	}
	if err := p.held(ctx); err != nil {
		return catalog, source, err
	}
	if intended != nil {
		if !p.startup || p.scope == nil || intended.Default == nil || intended.Default.Workflow != 2 || intended.Default.OwnSelf != own || intended.Default.Selected != native || string(intended.Default.SelectedPath) != path || string(intended.Default.Next) != next || intended.Default.CreationPage != page || bridge != (storagebudget.ID([16]byte(p.scope[24:40]))) {
			return catalog, source, storagebudget.ErrAuthority
		}
		fence, e := p.enrollmentEnvelope(p.initialFence, true)
		if e != nil || fence.Scope != *p.scope || fence.Snapshot == nil || fence.Snapshot.Intent == nil {
			return catalog, source, storagebudget.ErrBounds
		}
		raw, e := storageEnrollmentPeakEncodeSource(*intended)
		if e != nil || !bytes.Equal(raw, fence.Snapshot.Intent.Source) {
			return catalog, source, storagebudget.ErrBounds
		}
	}
	expected, err := storageHistoryNextComponent(path, s.defaultChatDataPath())
	if err != nil || expected != next {
		return catalog, source, errors.Join(errStorageCustody, err)
	}
	var settingsNative storageOwnerPersistentObject
	var references []storagebudget.ID
	if intended != nil {
		if !p.c.sources.historyAncestorSelected(ctx, *intended) {
			return catalog, source, errStorageCustody
		}
		found := false
		for _, row := range p.previousEvidence.Inner.Catalog.Roots {
			if row.Role == 1 && int(row.DirectoryObject) < len(p.previousEvidence.Inner.Catalog.Objects) && p.previousEvidence.Inner.Catalog.Objects[row.DirectoryObject].Persistent == intended.Default.Settings {
				if found {
					return catalog, source, errStorageCustody
				}
				found = true
				settingsNative = intended.Default.Settings
				references = append([]storagebudget.ID{}, row.References...)
			}
		}
		if !found {
			return catalog, source, errStorageCustody
		}
	} else {
		var settings *storageRootBinding
		var selection *storageOriginalHistorySelection
		if p.startup {
			settings, selection, err = p.c.originalHistorySettings(ctx, s)
		} else {
			settings, selection, err = p.c.historySettings(ctx, s)
		}
		if err != nil || selection == nil || selection.explicit {
			return catalog, source, errors.Join(errStorageCustody, err)
		}
		settingsNative = settings.source.persistent
		references = append([]storagebudget.ID{}, settings.references...)
	}
	before := p.previousEvidence.Inner.Catalog
	raw, err := json.Marshal(before)
	if err != nil {
		return catalog, source, err
	}
	if err = storageEnrollmentPeakCatalogJSON(raw, &catalog); err != nil {
		return catalog, source, err
	}
	if catalog.Sequence == ^uint64(0) || p.initialInspection.View.Configuration.Revision == ^uint64(0) {
		return catalog, source, storagebudget.ErrBounds
	}
	catalog.Version = 1
	catalog.Sequence++
	catalog.Phase = "prepared"
	catalog.ExpectedQRevision = p.initialInspection.View.Configuration.Revision + 1
	if own == 0 && intended == nil {
		bridge, err = storagebudget.NewID()
		if err != nil {
			return catalog, source, err
		}
	}
	catalog.Transition = bridge
	// The inner catalog continues the selected original core. Physical bank
	// checksums remain separate custody/publication operands, not this link.
	cores, bank, err := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil {
		return catalog, source, err
	}
	copy(catalog.PreviousCatalogDigest[:], cores[bank][32:64])
	catalog.BeforeDefinitionIDs = storageOwnerDefinitionIDs{Roots: []storagebudget.ID{}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{}, Constraints: []storagebudget.ID{}}
	for _, r := range before.Roots {
		catalog.BeforeDefinitionIDs.Roots = append(catalog.BeforeDefinitionIDs.Roots, r.Definition.ID)
	}
	for _, r := range before.Workspaces {
		catalog.BeforeDefinitionIDs.Workspaces = append(catalog.BeforeDefinitionIDs.Workspaces, r.Definition.ID)
	}
	for _, r := range before.Filesystems {
		catalog.BeforeDefinitionIDs.Filesystems = append(catalog.BeforeDefinitionIDs.Filesystems, r.Definition.ID)
	}
	for _, r := range before.Constraints {
		catalog.BeforeDefinitionIDs.Constraints = append(catalog.BeforeDefinitionIDs.Constraints, r.Definition.ID)
	}
	key, err := p.c.storageAccountingKey(ctx, s)
	if err != nil {
		return catalog, source, err
	}
	var workspace storageOwnerWorkspace
	for _, w := range catalog.Workspaces {
		if string(w.OwnerKey) == key && string(w.HistoryIdentity) == s.conversationIdentity() {
			if workspace.Definition.ID != (storagebudget.ID{}) {
				return catalog, source, errStorageCustody
			}
			workspace = w
		}
	}
	if workspace.Definition.ID == (storagebudget.ID{}) {
		return catalog, source, errStorageCustody
	}
	profile, err := storageHostInstalledProfile(native.Backend)
	if err != nil {
		return catalog, source, err
	}
	installed := false
	for _, old := range catalog.Profiles {
		installed = installed || old == profile
	}
	if !installed {
		return catalog, source, storagebudget.ErrUnsupported
	}
	advisory := storageOwnerAdvisoryConstraint(catalog.Installation).Definition
	var fs storageOwnerFilesystem
	for _, old := range catalog.Filesystems {
		if old.Backend == native.Backend && old.VolumeUUID == native.VolumeUUID {
			if fs.Definition.ID != (storagebudget.ID{}) {
				return catalog, source, errStorageCustody
			}
			fs = old
		}
	}
	if fs.Definition.ID == (storagebudget.ID{}) {
		identity, e := storageOwnerDigest("desk-storage-original-volume-v3", native.VolumeUUID)
		if e != nil {
			return catalog, source, e
		}
		definition := storagebudget.Filesystem{ID: storageEnrollmentProjectDefinitionID("filesystem", catalog.Installation, bridge, native), Identity: identity, Generation: 1, InodeModel: storagebudget.InodesFinite, CapabilityEvidence: profile.InodeEvidence, ByteConstraint: advisory.ID, InodeConstraint: advisory.ID}
		if profile.Backend == 2 {
			definition.InodeModel = storagebudget.InodesDynamic
			definition.InodeConstraint = storagebudget.ID{}
		}
		fs = storageOwnerFilesystem{Definition: definition, Backend: profile.Backend, VolumeUUID: native.VolumeUUID, PersistentProfile: profile.ID, InodeProfile: profile.ID}
		catalog.Filesystems = append(catalog.Filesystems, fs)
	}
	for _, object := range catalog.Objects {
		if object.Persistent == native {
			return catalog, source, errStorageCustody
		}
	}
	index := uint32(len(catalog.Objects))
	catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: native, OwnerClass: 2, ModeClass: 1})
	id := storageEnrollmentProjectDefinitionID("history-ancestor", catalog.Installation, bridge, native)
	row := storageOwnerRoot{Definition: storagebudget.RegisteredRoot{ID: id, Owner: catalog.QBinding, Filesystem: fs.Definition.ID, Generation: 1, Locator: "history-ancestor:" + storageHexID(id), CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: advisory.ID, Generation: advisory.Generation}}}, Role: 8, Category: storagebudget.Control, References: append([]storagebudget.ID{}, references...), Source: storageOwnerRootSource{Kind: 9, OwnerKey: append([]byte{}, workspace.OwnerKey...), Path: []byte(path), Relative: []byte{}, ControlIndex: storageOwnerNoControl}, DirectoryObject: index, DomainObject: storageOwnerNoObject, WorkObject: storageOwnerNoObject, Membership: storageOwnerMembership{Backend: profile.Backend, ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileEvidence: profile.Evidence, Pool: fs.Definition.ID, VolumeUUID: native.VolumeUUID, Creation: 1, ExistingObjects: []uint32{}}}
	if len(row.References) == 1 {
		row.Definition.Workspace = row.References[0]
	}
	if profile.Backend == 1 {
		row.Membership.User = storageOwnerSubject{State: 4, Kind: 1}
		row.Membership.Group = storageOwnerSubject{State: 4, Kind: 2}
		row.Membership.Project = storageOwnerSubject{State: 4, Kind: 3}
	}
	source = storageEnrollmentPeakSource{Installation: catalog.Installation, QBinding: catalog.QBinding, Project: p.original.Project, Workspace: workspace.Definition.ID, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: append([]byte{}, workspace.OwnerKey...), HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), Filesystem: fs.Definition.ID, FilesystemIdentity: fs.Definition.Identity, FilesystemGeneration: fs.Definition.Generation, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Generation: 1, Namespace: []byte("history-base"), Default: &storageEnrollmentDefaultSource{Workflow: 2, Branch: storageCreationPersonal, Settings: settingsNative, Selected: native, SelectedPath: []byte(path), OwnSelf: own, CreationPage: page, Next: []byte(next)}}
	source.HomeSource, err = storageEnrollmentHomeSourceDigest(source, catalog)
	if err != nil {
		return catalog, source, err
	}
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, catalog, row)
	if err != nil {
		return catalog, source, err
	}
	row.Source.Relative, err = storageEnrollmentPeakEncodeSource(source)
	if err != nil {
		return catalog, source, err
	}
	row.Definition.Identity = storageEnrollmentPeakHash("desk-default-history-ancestor-scope-e1", row.Source.Relative)
	constraints := []storagebudget.CapacityConstraint{}
	for _, v := range catalog.Constraints {
		constraints = append(constraints, v.Definition)
	}
	row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(row.Definition), fs.Definition, constraints)
	if err != nil {
		return catalog, source, err
	}
	catalog.Roots = append(catalog.Roots, row)
	if err = storageEnrollmentSortObjects(&catalog); err != nil {
		return catalog, source, err
	}
	sort.Slice(catalog.Roots, func(i, j int) bool {
		return bytes.Compare(catalog.Roots[i].Definition.ID[:], catalog.Roots[j].Definition.ID[:]) < 0
	})
	sort.Slice(catalog.Filesystems, func(i, j int) bool {
		return bytes.Compare(catalog.Filesystems[i].Definition.ID[:], catalog.Filesystems[j].Definition.ID[:]) < 0
	})
	a, b, err := catalog.definitions()
	if err != nil {
		return catalog, source, err
	}
	catalog.BeforeDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", a)
	if err != nil {
		return catalog, source, err
	}
	catalog.AfterDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", b)
	if err != nil {
		return catalog, source, err
	}
	if err = storageEnrollmentPeakValidateCatalog(catalog, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim); err != nil {
		return catalog, source, err
	}
	if !storageEnrollmentRetainsOriginal(before, catalog) {
		return catalog, source, errStorageCustody
	}
	if intended != nil {
		actual, e := storageEnrollmentPeakEncodeSource(source)
		if e != nil {
			return catalog, source, e
		}
		want, e := storageEnrollmentPeakEncodeSource(*intended)
		if e != nil || !bytes.Equal(actual, want) {
			return catalog, source, errStorageCustody
		}
	}
	return catalog, source, p.held(ctx)
}

// Transfer only the actual creator-returned description. This is intentionally
// separate from reopening an already completed catalog during startup.
func (m *storageCreationLive) retainCreatedAncestorSource(ctx context.Context, catalog storageOwnerCatalogV2, binding storageEnrollmentPeakSource) (err error) {
	if m == nil || m.self != m || m.record.Kind != storageCreationAncestor || m.record.Core.Phase != storageIntentRetired || len(m.record.Effects) != 1 || binding.Default == nil || binding.Default.OwnSelf != 1 || binding.Default.Selected != m.record.Effects[0].Native || binding.Default.CreationPage != sha256.Sum256(m.selected.page) {
		return storagebudget.ErrAuthority
	}
	if err = m.current(ctx); err != nil {
		return err
	}
	if err = m.objectCurrent(ctx, 0, true); err != nil {
		return err
	}
	object := &m.objects[0]
	root, err := object.root.OpenRoot(".")
	if err != nil {
		return err
	}
	accepted := false
	defer func() {
		if !accepted {
			err = errors.Join(err, root.Close())
		}
	}()
	named := storageNamedRoot{parent: object.parent, name: object.name, root: root, identity: object.identity, private: true}
	if err = named.current(); err != nil {
		return err
	}
	var row *storageOwnerRoot
	for i := range catalog.Roots {
		r := &catalog.Roots[i]
		if r.Role == 8 && catalog.Objects[r.DirectoryObject].Persistent == object.native {
			if row != nil {
				return errStorageCustody
			}
			row = r
		}
	}
	if row == nil || !storageEnrollmentPeakSourceJoins(binding, catalog, *row) {
		return errStorageCustody
	}
	x := m.phase.c.sources
	if len(x.roots) >= 4096 {
		return storagebudget.ErrBounds
	}
	for _, old := range x.roots {
		if old.persistent == object.native {
			return errStorageCustody
		}
	}
	source := &storageOriginalSourceRoot{root: root, witnesses: []storageNamedRoot{named}, path: string(binding.Default.SelectedPath), key: string(binding.OwnerKey), role: 8, category: storagebudget.Control, persistent: object.native, relative: string(binding.Default.Next), workspaces: append([]storagebudget.ID{}, row.References...), scope: &binding}
	if err = source.current(ctx, m.phase.c); err != nil {
		return err
	}
	if err = m.objectCurrent(ctx, 0, true); err != nil {
		return err
	}
	x.roots = append(x.roots, source)
	x.openedRoots = append(x.openedRoots, root)
	accepted = true
	return nil
}

// Translate dictionary references only for an admitted ancestry append. Every
// old object and control has one exact semantic image, including parent
// references; the sole additional object is the declared ancestry boundary.
func storageEnrollmentHistoryAncestorObjectMapping(before, after storageOwnerCatalogV2, source storageEnrollmentPeakSource) bool {
	if source.Default == nil || source.Default.Workflow != 2 || !storageEnrollmentDefaultSourceNamespace(source) || len(after.Objects) != len(before.Objects)+1 || len(after.OwnerControls) != len(before.OwnerControls) {
		return false
	}
	indices := map[storageOwnerPersistentObject]uint32{}
	for i, object := range after.Objects {
		if _, duplicate := indices[object.Persistent]; duplicate {
			return false
		}
		indices[object.Persistent] = uint32(i)
	}
	prior := map[storageOwnerPersistentObject]bool{}
	matched := make([]bool, len(after.Objects))
	for _, old := range before.Objects {
		if prior[old.Persistent] {
			return false
		}
		prior[old.Persistent] = true
		index, ok := indices[old.Persistent]
		if !ok || matched[index] {
			return false
		}
		matched[index] = true
		expected := old
		if old.Parent != storageOwnerNoObject {
			if int(old.Parent) >= len(before.Objects) {
				return false
			}
			var ok bool
			expected.Parent, ok = indices[before.Objects[old.Parent].Persistent]
			if !ok {
				return false
			}
		}
		if !reflect.DeepEqual(expected, after.Objects[index]) {
			return false
		}
	}
	added := 0
	for i, ok := range matched {
		if !ok {
			added++
			expected := storageOwnerObject{Role: 20, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: source.Default.Selected, OwnerClass: 2, ModeClass: 1}
			if !reflect.DeepEqual(after.Objects[i], expected) {
				return false
			}
		}
	}
	if added != 1 {
		return false
	}
	controls := make([]bool, len(after.OwnerControls))
	for _, old := range before.OwnerControls {
		if int(old.ParentObject) >= len(before.Objects) {
			return false
		}
		parent, ok := indices[before.Objects[old.ParentObject].Persistent]
		if !ok {
			return false
		}
		expected := old
		expected.ParentObject = parent
		found := -1
		for i, next := range after.OwnerControls {
			if next.ParentObject == parent && bytes.Equal(next.Name, old.Name) {
				if found >= 0 || controls[i] || !reflect.DeepEqual(expected, next) {
					return false
				}
				found = i
			}
		}
		if found < 0 {
			return false
		}
		controls[found] = true
		a, e := before.controlDigest(old)
		b, f := after.controlDigest(after.OwnerControls[found])
		if e != nil || f != nil || a != b || a != old.RelationDigest {
			return false
		}
	}
	for _, ok := range controls {
		if !ok {
			return false
		}
	}
	return true
}
func (x *storageOriginalSources) admittedHistoryAncestorObjectIndices(ctx context.Context, catalog storageOwnerCatalogV2) bool {
	if x == nil || x.construction == nil || x.construction.owner == nil {
		return false
	}
	gate := &x.construction.owner.admission
	gate.mu.Lock()
	p := gate.enrollment
	gate.mu.Unlock()
	if p == nil || p.c != x.construction || p.scope == nil || p.initialPacket == nil {
		return false
	}
	e := p.initialPacket
	if e.Scope != *p.scope || e.Board.SourceKind != 10 || e.Board.Source.Default == nil || e.Board.Source.Default.Workflow != 2 {
		return false
	}
	var before storageOwnerCatalogV2
	found := false
	for _, projection := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(projection) == e.Before {
			if found {
				return false
			}
			found = true
			before = projection.Catalog
		}
	}
	after := e.Inner.Catalog
	source := e.Board.Source
	if !found || !reflect.DeepEqual(catalog, before) && !reflect.DeepEqual(catalog, after) || !storageEnrollmentHistoryAncestorObjectMapping(before, after, source) || !storageEnrollmentRetainsOriginal(before, after) {
		return false
	}
	if len(after.Roots) != len(before.Roots)+1 || len(after.Workspaces) != len(before.Workspaces) || len(after.Constraints) != len(before.Constraints) || len(after.Profiles) != len(before.Profiles) || len(after.Filesystems) != len(before.Filesystems)+len(e.Board.Registration[2]) || len(e.Board.Registration[0]) != 1 || len(e.Board.Registration[1]) != 0 || len(e.Board.Registration[2]) > 1 || len(e.Board.Registration[3]) != 0 {
		return false
	}
	index := int(e.Board.Registration[0][0])
	if index >= len(after.Roots) {
		return false
	}
	row := after.Roots[index]
	if row.Role != 8 || row.Source.Kind != 9 || int(row.DirectoryObject) >= len(after.Objects) || after.Objects[row.DirectoryObject].Persistent != source.Default.Selected || !storageEnrollmentPeakSourceJoins(source, after, row) {
		return false
	}
	encoded, err := storageEnrollmentPeakEncodeSource(source)
	if err != nil || !bytes.Equal(row.Source.Relative, encoded) {
		return false
	}
	// The ancestry append preserves the complete original config reference
	// census. It cannot append an unrelated root, workspace, or capacity row.
	settings := false
	for _, old := range before.Roots {
		if int(old.DirectoryObject) >= len(before.Objects) {
			return false
		}
		if old.Role == 1 && before.Objects[old.DirectoryObject].Persistent == source.Default.Settings {
			if settings || !reflect.DeepEqual(old.References, row.References) {
				return false
			}
			settings = true
		}
	}
	if !settings {
		return false
	}
	if len(e.Board.Registration[2]) == 1 {
		index := int(e.Board.Registration[2][0])
		if index >= len(after.Filesystems) {
			return false
		}
		fs := after.Filesystems[index]
		if fs.Definition.ID != row.Definition.Filesystem || fs.Backend != source.Default.Selected.Backend || fs.VolumeUUID != source.Default.Selected.VolumeUUID {
			return false
		}
	}
	return x.historyAncestorSelected(ctx, source)
}
