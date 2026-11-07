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

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// OriginalOwner's only dictionary reference is RegistryControl. Translating
// that index preserves the exact registry relation, including its parent
// native, owner, payload relation and bound. Every other owner field is exact.
func storageEnrollmentOriginalOwnerEqual(before, after storageOwnerCatalogV2, source *storageEnrollmentPeakSource) bool {
	a, b := before.OriginalOwner, after.OriginalOwner
	if reflect.DeepEqual(a, b) {
		return a.Kind != 2 || storageEnrollmentOriginalRegistryEqual(before, after)
	}
	if source != nil && source.Default != nil && source.Default.Workflow == 5 {
		if a.Kind != b.Kind || a.Kind != 2 || !storageEnrollmentOriginalRegistryEqual(before, after) {
			return false
		}
		a.RegistryControl = 0
		b.RegistryControl = 0
		return reflect.DeepEqual(a, b)
	}
	if source == nil || source.Default == nil || source.Default.Workflow != 1 && source.Default.Workflow != 3 {
		return false
	}
	// The sole reindex exception is the admitted one-control append. Compare
	// every old control's complete material before translating any owner index.
	if !storageEnrollmentDefaultControlAppendEqual(before, after, source) {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == 2 {
		if !storageEnrollmentOriginalRegistryEqual(before, after) {
			return false
		}
		a.RegistryControl = 0
		b.RegistryControl = 0
	}
	return reflect.DeepEqual(a, b)
}
func storageEnrollmentDefaultControlAppendEqual(before, after storageOwnerCatalogV2, source *storageEnrollmentPeakSource) bool {
	if source == nil || source.Default == nil || source.Default.Workflow != 1 && source.Default.Workflow != 3 {
		return false
	}
	low, high := before, after
	if len(low.OwnerControls) > len(high.OwnerControls) {
		low, high = high, low
	}
	if len(high.OwnerControls) != len(low.OwnerControls)+1 {
		return false
	}
	matched := make([]bool, len(high.OwnerControls))
	for _, old := range low.OwnerControls {
		if int(old.ParentObject) >= len(low.Objects) {
			return false
		}
		found := -1
		for i, next := range high.OwnerControls {
			if int(next.ParentObject) >= len(high.Objects) {
				return false
			}
			if low.Objects[old.ParentObject].Persistent != high.Objects[next.ParentObject].Persistent || !bytes.Equal(old.Name, next.Name) {
				continue
			}
			if found >= 0 || matched[i] || old.MaxBytes != next.MaxBytes || old.RelationDigest != next.RelationDigest {
				return false
			}
			x, e := low.controlDigest(old)
			y, f := high.controlDigest(next)
			if e != nil || f != nil || x != y {
				return false
			}
			found = i
		}
		if found < 0 {
			return false
		}
		matched[found] = true
	}
	for i, ok := range matched {
		if !ok {
			next := high.OwnerControls[i]
			d := source.Default
			owner := high.OriginalOwner.HomeOwner
			if d.Branch == storageCreationContained {
				owner = string(source.OwnerKey)
				if high.OriginalOwner.Kind == 2 {
					owner = ""
					for _, entry := range high.OriginalOwner.Entries {
						if entry.RunnerWorkspace == string(source.OwnerKey) && bytes.Equal(entry.HistoryIdentity, source.HistoryIdentity) {
							if owner != "" {
								return false
							}
							owner = entry.StorageOwner
						}
					}
				}
			}
			relation, err := high.controlDigest(next)
			if owner == "" || next.Role != 2 || next.OwnerKind != 2 || string(next.OwnerKey) != owner || err != nil || relation != next.RelationDigest || high.Objects[next.ParentObject].Persistent != d.Settings || string(next.Name) != dataLocationName || next.Location.Version != 1 || !bytes.Equal(next.Location.Path, d.SelectedPath) || len(next.Location.Previous) != 0 || next.Location.Selected != d.Selected || next.Location.PreviousSelected != (storageOwnerPersistentObject{}) || next.MaxBytes != 16384 {
				return false
			}
		}
	}
	return true
}

func storageEnrollmentOriginalRegistryEqual(before, after storageOwnerCatalogV2) bool {
	a, b := before.OriginalOwner, after.OriginalOwner
	if a.Kind != 2 || b.Kind != 2 || int(a.RegistryControl) >= len(before.OwnerControls) || int(b.RegistryControl) >= len(after.OwnerControls) {
		return false
	}
	old, next := before.OwnerControls[a.RegistryControl], after.OwnerControls[b.RegistryControl]
	if old.Role != 1 || next.Role != 1 || int(old.ParentObject) >= len(before.Objects) || int(next.ParentObject) >= len(after.Objects) || before.Objects[old.ParentObject].Persistent != after.Objects[next.ParentObject].Persistent || old.MaxBytes != next.MaxBytes || old.RelationDigest != next.RelationDigest {
		return false
	}
	x, e := before.controlDigest(old)
	y, f := after.controlDigest(next)
	return e == nil && f == nil && x == y
}
func (x *storageOriginalSources) originalOwnerEqual(catalog storageOwnerCatalogV2) bool {
	if x == nil {
		return false
	}
	original := storageOwnerCatalogV2{OriginalOwner: x.owner, Objects: x.objects, OwnerControls: x.controls}
	if storageEnrollmentOriginalOwnerEqual(original, catalog, nil) {
		return true
	}
	if x.construction != nil {
		if op := x.construction.originalTransferOperation(); op != nil && op.record.Phase == storageTransferRetired && op.record.PointerState == 2 && storageTransferSelectionRelation(op.record, op.catalog, catalog) == nil {
			a, b := original.OriginalOwner, catalog.OriginalOwner
			if a.Kind == 2 && b.Kind == 2 && storageEnrollmentOriginalRegistryEqual(original, catalog) {
				a.RegistryControl = 0
				b.RegistryControl = 0
			}
			return reflect.DeepEqual(a, b)
		}
	}
	source := x.admittedDefaultSource(catalog)
	if source == nil {
		return false
	}
	return storageEnrollmentOriginalOwnerEqual(original, catalog, source)
}
func (x *storageOriginalSources) admittedDefaultSource(catalog storageOwnerCatalogV2) *storageEnrollmentPeakSource {
	if x == nil {
		return nil
	}
	if x.construction == nil || x.construction.owner == nil {
		return nil
	}
	gate := &x.construction.owner.admission
	gate.mu.Lock()
	p := gate.enrollment
	gate.mu.Unlock()
	if p == nil || p.c != x.construction || p.scope == nil || p.initialPacket == nil {
		return nil
	}
	e := p.initialPacket
	allowed := reflect.DeepEqual(catalog, e.Inner.Catalog)
	for _, projection := range e.Projections {
		if storageEnrollmentPeakProjectionFullDigest(projection) == e.Before && reflect.DeepEqual(catalog, projection.Catalog) {
			allowed = true
		}
	}
	if !allowed || e.Board.Source.Default == nil || e.Board.Source.Default.Workflow != 1 && e.Board.Source.Default.Workflow != 3 {
		return nil
	}
	return &e.Board.Source
}

func storageEnrollmentSortControls(c *storageOwnerCatalogV2) error {
	type indexed struct {
		old uint32
		row storageOwnerControl
	}
	rows := make([]indexed, len(c.OwnerControls))
	for i, row := range c.OwnerControls {
		if int(row.ParentObject) >= len(c.Objects) {
			return storagebudget.ErrBounds
		}
		rows[i] = indexed{uint32(i), row}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i].row, rows[j].row
		n := storageOwnerPOCompare(c.Objects[a.ParentObject].Persistent, c.Objects[b.ParentObject].Persistent)
		return n < 0 || n == 0 && bytes.Compare(a.Name, b.Name) < 0
	})
	indices := make([]uint32, len(rows))
	for i, row := range rows {
		c.OwnerControls[i] = row.row
		indices[row.old] = uint32(i)
	}
	if c.OriginalOwner.Kind == 2 {
		if int(c.OriginalOwner.RegistryControl) >= len(indices) {
			return storagebudget.ErrBounds
		}
		c.OriginalOwner.RegistryControl = indices[c.OriginalOwner.RegistryControl]
	}
	for i := range c.Roots {
		r := &c.Roots[i]
		if r.Source.ControlKind == 2 {
			if int(r.Source.ControlIndex) >= len(indices) {
				return storagebudget.ErrBounds
			}
			r.Source.ControlIndex = indices[r.Source.ControlIndex]
		}
	}
	return nil
}

// A duplicate of a still-held created object is a lifetime transfer, not
// adoption by matching its pathname. The exact returned creator descriptor,
// named edge and recorded native are checked on both sides of the duplicate.
func (m *storageCreationLive) retainCreatedHistorySource(ctx context.Context, row storageOwnerRoot) (out *storageOriginalSourceRoot, err error) {
	if m == nil || m.self != m || m.record.Kind != storageCreationHistory || m.record.Core.Phase != storageIntentRetired || row.Role != 4 || row.Source.Kind != 4 {
		return nil, storagebudget.ErrAuthority
	}
	if err = m.current(ctx); err != nil {
		return nil, err
	}
	selected := -1
	marker := -1
	for i, effect := range m.record.Effects {
		if effect.Kind == 1 && (effect.Name == "data" || effect.Name == "history") {
			selected = i
		}
		if effect.Name == dataMarkerName {
			marker = i
		}
	}
	if selected < 0 || marker < 0 {
		return nil, storagebudget.ErrAuthority
	}
	if err = m.objectCurrent(ctx, selected, true); err != nil {
		return nil, err
	}
	if err = m.objectCurrent(ctx, marker, true); err != nil {
		return nil, err
	}
	object := &m.objects[selected]
	originalMarker := &m.objects[marker]
	if object.root == nil || originalMarker.parent != object.root || object.native != m.record.Effects[selected].Native {
		return nil, errStorageCustody
	}
	root, err := object.root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	accepted := false
	var mark storageNamedFile
	defer func() {
		if !accepted {
			if mark.file != nil {
				err = errors.Join(err, mark.file.Close())
			}
			err = errors.Join(err, root.Close())
		}
	}()
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(info, object.identity) {
		return nil, errors.Join(errStorageCustody, err)
	}
	edge := storageNamedRoot{parent: object.parent, name: object.name, root: root, identity: object.identity, private: true}
	if err = edge.current(); err != nil {
		return nil, err
	}
	mark, err = storageOpenFile(root, dataMarkerName, int64(m.record.Effects[marker].Bytes))
	if err != nil {
		return nil, err
	}
	if !os.SameFile(mark.identity, originalMarker.identity) {
		return nil, errStorageCustody
	}
	native, err := storageOwnerPersistentFile(ctx, mark)
	if err != nil || native != m.record.Effects[marker].Native {
		return nil, errors.Join(errStorageCustody, err)
	}
	mark.immutable = append([]byte{}, m.payloads[marker]...)
	source := &storageOriginalSourceRoot{root: root, witnesses: []storageNamedRoot{edge}, path: m.record.SelectedPath, key: string(row.Source.OwnerKey), role: 4, category: storagebudget.History, persistent: object.native, marker: &mark, workspaces: append([]storagebudget.ID{}, row.References...)}
	if err = m.objectCurrent(ctx, selected, true); err != nil {
		return nil, err
	}
	if err = m.objectCurrent(ctx, marker, true); err != nil {
		return nil, err
	}
	x := m.phase.c.sources
	if len(x.roots) >= 4096 {
		return nil, storagebudget.ErrBounds
	}
	for _, old := range x.roots {
		if old.persistent == source.persistent {
			return nil, errStorageCustody
		}
	}
	x.roots = append(x.roots, source)
	x.openedRoots = append(x.openedRoots, root)
	x.openedFiles = append(x.openedFiles, mark.file)
	accepted = true
	out = source
	return out, out.current(ctx, m.phase.c)
}

// This builder consumes the actual retired member, not a decoded J4 offered
// by a caller. It adds the single final History/control relation; existing
// root definitions and their membership/control operands remain unchanged.
func (m *storageCreationLive) planCreatedHistoryCatalog(ctx context.Context) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	var catalog storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if m == nil || m.self != m || m.phase == nil || m.record.Kind != storageCreationHistory {
		return catalog, source, storagebudget.ErrAuthority
	}
	if err := m.current(ctx); err != nil {
		return catalog, source, err
	}
	parents := make([]*storageRootBinding, len(m.parents))
	for i, parent := range m.parents {
		parents[i] = parent.binding
	}
	workspace, owner, err := m.phase.historyCreationOwner(ctx, m.record, parents)
	if err != nil {
		return catalog, source, err
	}
	if string(owner) != m.requester.storageOwner() {
		return catalog, source, errStorageCustody
	}
	catalog, source, err = m.phase.planHistoryCatalog(ctx, m.record, m.selected.page, parents, workspace, owner)
	if err != nil {
		return catalog, source, err
	}
	created := false
	for _, effect := range m.record.Effects {
		created = created || effect.Kind == 1 && effect.Native == source.Default.Selected
	}
	if created {
		for _, row := range catalog.Roots {
			if row.Role == 4 && catalog.Objects[row.DirectoryObject].Persistent == source.Default.Selected {
				if _, err = m.retainCreatedHistorySource(ctx, row); err != nil {
					return catalog, source, err
				}
			}
		}
	}
	return catalog, source, nil
}

// Both callers derive workspace/owner from the original settings reference
// relation. A registered child needs no recreated Server or caller wrapper at
// restart; original registry/native custody remains with this construction.
func (p *storageEnrollmentEpoch) historyCreationOwner(ctx context.Context, record storageCreationIntent, parents []*storageRootBinding) (storageOwnerWorkspace, []byte, error) {
	var workspace storageOwnerWorkspace
	var owner []byte
	if p == nil || p.previousEvidence == nil || len(parents) != len(record.Parents) {
		return workspace, owner, storagebudget.ErrAuthority
	}
	var settings *storageRootBinding
	for i, parent := range record.Parents {
		if parent.Role == 5 {
			if settings != nil {
				return workspace, owner, errStorageCustody
			}
			settings = parents[i]
		}
	}
	if settings == nil || settings.source == nil {
		return workspace, owner, errStorageCustody
	}
	catalog := p.previousEvidence.Inner.Catalog
	if record.Branch == storageCreationContained {
		if len(settings.references) != 1 || settings.source.role != 3 && settings.source.role != 6 {
			return workspace, owner, errStorageCustody
		}
		for _, w := range catalog.Workspaces {
			if w.Definition.ID == settings.references[0] {
				if workspace.Definition.ID != (storagebudget.ID{}) {
					return workspace, owner, errStorageCustody
				}
				workspace = w
			}
		}
		if catalog.OriginalOwner.Kind == 2 {
			for _, entry := range catalog.OriginalOwner.Entries {
				if entry.RunnerWorkspace == string(workspace.OwnerKey) && bytes.Equal(entry.HistoryIdentity, workspace.HistoryIdentity) {
					if owner != nil {
						return workspace, nil, errStorageCustody
					}
					owner = []byte(entry.StorageOwner)
				}
			}
		} else {
			owner = []byte(p.c.server.storageOwner())
		}
	} else {
		if !settings.configControl || settings.source.persistent != record.Tail.Config {
			return workspace, owner, errStorageCustody
		}
		key, err := p.c.storageAccountingKey(ctx, p.c.server)
		if err != nil {
			return workspace, owner, err
		}
		for _, w := range catalog.Workspaces {
			if string(w.OwnerKey) == key && string(w.HistoryIdentity) == p.c.server.conversationIdentity() {
				if workspace.Definition.ID != (storagebudget.ID{}) {
					return workspace, owner, errStorageCustody
				}
				workspace = w
			}
		}
		owner = []byte(catalog.OriginalOwner.HomeOwner)
	}
	if workspace.Definition.ID == (storagebudget.ID{}) || len(owner) == 0 {
		return workspace, owner, errStorageCustody
	}
	return workspace, owner, p.held(ctx)
}

// Pure planning is shared with qualified startup comparison. This function
// cannot issue a member, reconstruct call authority or perform an effect.
func (p *storageEnrollmentEpoch) planHistoryCatalog(ctx context.Context, record storageCreationIntent, page []byte, parents []*storageRootBinding, workspace storageOwnerWorkspace, owner []byte) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	var zero storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if p == nil || p.previousEvidence == nil || len(parents) != len(record.Parents) {
		return zero, source, storagebudget.ErrAuthority
	}
	rows, err := storageCreationOriginalParentRows(record, p.previousEvidence.Inner.Catalog)
	if err != nil {
		return zero, source, err
	}
	for i, parent := range parents {
		if parent == nil || parent.source == nil || parent.row.ID != rows[i].Definition.ID || !reflect.DeepEqual(parent.references, rows[i].References) {
			return zero, source, errStorageCustody
		}
		if err = parent.current(ctx); err != nil {
			return zero, source, err
		}
	}
	return p.planHistoryCatalogOriginal(ctx, record, page, rows, workspace, owner)
}

// Parent rows are complete comparison operands from the exact prior catalog;
// this pure constructor does not manufacture storageRootBinding authority.
func (p *storageEnrollmentEpoch) planHistoryCatalogOriginal(ctx context.Context, record storageCreationIntent, page []byte, parents []storageOwnerRoot, workspace storageOwnerWorkspace, owner []byte) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	var catalog storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if p == nil || p.previousEvidence == nil || p.initialInspection == nil || record.Kind != storageCreationHistory || record.Core.Phase != storageIntentRetired || len(parents) != len(record.Parents) || p.initialInspection.View.Configuration.Epoch != record.Core.RetirementEpoch {
		return catalog, source, storagebudget.ErrAuthority
	}
	if encoded, err := storageEncodeCreationIntent(record, record.Tail.Installation, record.Tail.QBinding); err != nil || !bytes.Equal(encoded, page) {
		return catalog, source, errors.Join(errStorageCustody, err)
	}
	if err := p.held(ctx); err != nil {
		return catalog, source, err
	}
	rows, err := storageCreationOriginalParentRows(record, p.previousEvidence.Inner.Catalog)
	if err != nil || !reflect.DeepEqual(rows, parents) {
		return catalog, source, errStorageCustody
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
	catalog.Transition = record.Tail.Bridge
	catalog.ExpectedQRevision = p.initialInspection.View.Configuration.Revision + 1
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
	settings := storageOwnerPersistentObject{}
	selected := storageOwnerPersistentObject{}
	marker := storageOwnerPersistentObject{}
	location := storageCreationEffect{}
	selectedEffect := -1
	markerEffect := -1
	var template storageOwnerRoot
	var references []storagebudget.ID
	for i, parent := range record.Parents {
		if parent.Role == 5 {
			settings = parent.Native
			references = append([]storagebudget.ID{}, parents[i].References...)
		}
		if parent.Role == 6 {
			selected = parent.Native
		}
		if parent.Role == 4 || parent.Role == 6 {
			for _, r := range catalog.Roots {
				if r.Definition.ID == parents[i].Definition.ID {
					template = r
				}
			}
		}
	}
	for i, effect := range record.Effects {
		if effect.Kind == 1 && (effect.Name == "data" || effect.Name == "history") {
			selected = effect.Native
			selectedEffect = i
		}
		if effect.Name == dataMarkerName {
			marker = effect.Native
			markerEffect = i
		}
		if effect.Name == dataLocationName {
			location = effect
		}
	}
	if !settings.valid(1) || !selected.valid(1) || !location.Native.valid(2) || template.Definition.ID == (storagebudget.ID{}) {
		return catalog, source, errStorageCustody
	}
	settingsIndex := storageOwnerNoObject
	for i, object := range catalog.Objects {
		if object.Persistent == settings {
			settingsIndex = uint32(i)
		}
	}
	if settingsIndex == storageOwnerNoObject {
		return catalog, source, errStorageCustody
	}
	for _, control := range catalog.OwnerControls {
		if control.Role == 2 && control.ParentObject == settingsIndex && string(control.Name) == dataLocationName {
			return catalog, source, errStorageCustody
		}
	}
	controlIndex := uint32(len(catalog.OwnerControls))
	catalog.OwnerControls = append(catalog.OwnerControls, storageOwnerControl{Role: 2, ParentObject: settingsIndex, Name: []byte(dataLocationName), OwnerKind: 2, OwnerKey: owner, MaxBytes: 16384, Location: storageOwnerLocation{Version: 1, Path: []byte(record.SelectedPath), Previous: []byte{}, Selected: selected}})
	var row storageOwnerRoot
	newRoot := false
	for _, old := range catalog.Roots {
		if int(old.DirectoryObject) < len(catalog.Objects) && catalog.Objects[old.DirectoryObject].Persistent == selected {
			if row.Definition.ID != (storagebudget.ID{}) {
				return catalog, source, errStorageCustody
			}
			row = old
		}
	}
	if selectedEffect >= 0 {
		if row.Definition.ID != (storagebudget.ID{}) || markerEffect < 0 || selected.Backend != template.Membership.Backend || selected.VolumeUUID != template.Membership.VolumeUUID {
			return catalog, source, errStorageCustody
		}
		dir := uint32(len(catalog.Objects))
		catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 21, Source: 3, Parent: storageOwnerNoObject, Name: []byte{}, Persistent: selected, OwnerClass: 2, ModeClass: 1})
		mark := uint32(len(catalog.Objects))
		catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 23, Source: 3, Parent: dir, Name: []byte(dataMarkerName), Persistent: marker, OwnerClass: 2, ModeClass: 2, FixedLength: record.Effects[markerEffect].Bytes})
		row = template
		row.Definition.ID = storageEnrollmentProjectDefinitionID("history-root", catalog.Installation, catalog.Transition, selected)
		row.Definition.Generation = 1
		row.Definition.Locator = "default-history:" + storageHexID(row.Definition.ID)
		row.Role = 4
		row.Category = storagebudget.History
		row.DirectoryObject = dir
		row.WorkObject = storageOwnerNoObject
		// History uses the original installation data lock, including when its
		// namespace parent is a personal ancestor with its own directory domain.
		row.DomainObject = storageOwnerNoObject
		for i, object := range catalog.Objects {
			if object.Role == 18 {
				row.DomainObject = uint32(i)
			}
		}
		if row.DomainObject == storageOwnerNoObject {
			return catalog, source, errStorageCustody
		}
		row.References = references
		row.Definition.Workspace = storagebudget.ID{}
		if len(row.References) == 1 {
			row.Definition.Workspace = row.References[0]
		}
		row.Membership.ExistingObjects = []uint32{mark}
		row.Source = storageOwnerRootSource{Kind: 4, OwnerKey: owner, Path: []byte(record.SelectedPath), Relative: []byte{}, ControlKind: 2, ControlIndex: controlIndex, ControlSelection: 1}
		newRoot = true
	} else {
		if row.Definition.ID == (storagebudget.ID{}) || !record.legacyConfigSelection() && row.Role != 4 || record.legacyConfigSelection() && row.Role != 1 {
			return catalog, source, errStorageCustody
		}
		if !record.legacyConfigSelection() {
			for _, object := range catalog.Objects {
				if object.Role == 23 && object.Parent == row.DirectoryObject {
					marker = object.Persistent
				}
			}
		}
	}
	source = storageEnrollmentPeakSource{Installation: catalog.Installation, QBinding: catalog.QBinding, Project: p.original.Project, Workspace: workspace.Definition.ID, WorkspaceGeneration: workspace.Definition.Generation, OwnerKey: append([]byte{}, workspace.OwnerKey...), HistoryIdentity: append([]byte{}, workspace.HistoryIdentity...), Filesystem: row.Definition.Filesystem, CapacityCount: row.Definition.CapacityCount, CapacityRefs: row.Definition.CapacityRefs, Generation: row.Definition.Generation, Namespace: []byte("data"), Default: &storageEnrollmentDefaultSource{Workflow: 1, Branch: record.Branch, Settings: settings, Selected: selected, Marker: marker, Location: location.Native, LocationPayload: location.Payload, CreationPage: sha256.Sum256(page), SelectedPath: []byte(record.SelectedPath), Next: []byte{}}}
	if record.Branch == storageCreationPersonal {
		source.Namespace = []byte("history")
	}
	if record.legacyConfigSelection() {
		source.Namespace = []byte("legacy")
		source.Default.Workflow = 3
	}
	var fs storagebudget.Filesystem
	for _, f := range catalog.Filesystems {
		if f.Definition.ID == source.Filesystem {
			fs = f.Definition
			source.FilesystemIdentity = fs.Identity
			source.FilesystemGeneration = fs.Generation
		}
	}
	source.HomeSource, err = storageEnrollmentHomeSourceDigest(source, catalog)
	if err != nil {
		return catalog, source, err
	}
	source.CapacityBinding, err = storageEnrollmentPeakSourceMapping(source, catalog, row)
	if err != nil {
		return catalog, source, err
	}
	if newRoot {
		row.Definition.Identity, err = catalog.rootIdentity(row)
		if err != nil {
			return catalog, source, err
		}
		constraints := []storagebudget.CapacityConstraint{}
		for _, v := range catalog.Constraints {
			constraints = append(constraints, v.Definition)
		}
		row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(row.Definition), fs, constraints)
		if err != nil {
			return catalog, source, err
		}
		catalog.Roots = append(catalog.Roots, row)
	}
	// Sorting translates dictionary indices only, including the registry-control
	// index. The complete original owner/control material remains exact.
	if err = storageEnrollmentSortObjects(&catalog); err != nil {
		return catalog, source, err
	}
	if err = storageEnrollmentSortControls(&catalog); err != nil {
		return catalog, source, err
	}
	for i := range catalog.OwnerControls {
		v := &catalog.OwnerControls[i]
		v.RelationDigest, err = catalog.controlDigest(*v)
		if err != nil {
			return catalog, source, err
		}
	}
	sort.Slice(catalog.Roots, func(i, j int) bool {
		return bytes.Compare(catalog.Roots[i].Definition.ID[:], catalog.Roots[j].Definition.ID[:]) < 0
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
	if !storageEnrollmentRetainsOriginalWithDefault(before, catalog, &source) || !storageEnrollmentDefaultHistorySourceJoins(source, catalog) {
		return catalog, source, errStorageCustody
	}
	return catalog, source, p.held(ctx)
}

// The pending exception ends only after the actual final catalog/Q/census
// selected this exact retired member. Ordinary pointer currentness then owns
// the selection; closing creator descriptions cannot erase its custody.
func (m *storageCreationLive) completeHistoryPointer(ctx context.Context, e storageEnrollmentPeakEvidence) error {
	if m == nil || m.self != m || m.record.Kind != storageCreationHistory || !storageCreationTailIncluded(m.record, e) {
		return storagebudget.ErrAuthority
	}
	if err := m.current(ctx); err != nil {
		return err
	}
	found := -1
	for i, selection := range m.phase.c.sources.selections {
		if selection.pending == m {
			if found >= 0 {
				return errStorageCustody
			}
			if err := m.pendingPointerCurrent(ctx, selection); err != nil {
				return err
			}
			found = i
		}
	}
	if found < 0 {
		return errStorageCustody
	}
	selection := m.phase.c.sources.selections[found]
	selection.pending = nil
	selection.explicit = true
	selection.location = dataLocation{Version: 1, Path: m.record.SelectedPath}
	m.phase.c.sources.selections[found] = selection
	x := m.phase.c.sources
	if err := x.current(ctx); err != nil {
		return err
	}
	return x.installCompletedHistoryDictionary(ctx, e.Inner.Catalog, e.Board.Source)
}

func (x *storageOriginalSources) installCompletedHistoryDictionary(ctx context.Context, catalog storageOwnerCatalogV2, source storageEnrollmentPeakSource) error {
	if x == nil || x.construction == nil || source.Default == nil || source.Default.Workflow != 1 && source.Default.Workflow != 3 {
		return storagebudget.ErrAuthority
	}
	original := storageOwnerCatalogV2{OriginalOwner: x.owner, Objects: x.objects, OwnerControls: x.controls}
	if !storageEnrollmentOriginalOwnerEqual(original, catalog, &source) || !storageEnrollmentDefaultControlAppendEqual(original, catalog, &source) {
		return errStorageCustody
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	var dictionary storageOwnerCatalogV2
	if err = storageEnrollmentPeakCatalogJSON(raw, &dictionary); err != nil {
		return err
	}
	config := storageOwnerNoObject
	for i, object := range dictionary.Objects {
		if object.Role == 1 && object.Persistent == x.construction.ref.cell.ownerRecord.Config {
			if config != storageOwnerNoObject {
				return errStorageCustody
			}
			config = uint32(i)
		}
	}
	if config == storageOwnerNoObject {
		return errStorageCustody
	}
	x.owner = dictionary.OriginalOwner
	x.objects = dictionary.Objects
	x.controls = dictionary.OwnerControls
	x.configObject = config
	return x.current(ctx)
}

// The startup caller already has an explicit captured pointer. Ending its
// dictionary restoration requires the actual final C/Q/census and the same
// selected retired J4 witness; no live pending-pointer exception is invented.
func (p *storageEnrollmentEpoch) completeStartupHistoryDictionary(ctx context.Context, e storageEnrollmentPeakEvidence) error {
	if !p.startup || p.creation != nil || p.scope == nil || e.Scope != *p.scope || e.Phase != 2 || p.completedEvidence == nil || !bytes.Equal(p.completedEvidence.Raw, e.Raw) {
		return storagebudget.ErrAuthority
	}
	d := e.Board.Source.Default
	if d == nil || d.Workflow != 1 && d.Workflow != 3 {
		return storagebudget.ErrAuthority
	}
	found := false
	for _, w := range e.Witnesses {
		if w.Selected && w.Creation != nil && w.Digest == d.CreationPage {
			if found || w.Creation.Kind != storageCreationHistory || !storageCreationTailIncluded(*w.Creation, e) {
				return errStorageCustody
			}
			found = true
		}
	}
	if !found {
		return errStorageCustody
	}
	selected := false
	for _, selection := range p.c.sources.selections {
		native, err := storageOwnerPersistentRoot(ctx, selection.settings)
		if err != nil {
			return err
		}
		if native == d.Settings {
			if selected || !selection.explicit || selection.pending != nil || selection.location.Path != string(d.SelectedPath) || selection.location.Previous != "" {
				return errStorageCustody
			}
			selected = true
		}
	}
	if !selected {
		return errStorageCustody
	}
	if err := p.current(ctx); err != nil {
		return err
	}
	// The surrounding final C/Q/census classifier has selected this exact
	// Completed packet. Revalidate every captured effect/profile before either
	// dictionary branch; matching counts or owner fields alone never suffice.
	if err := p.comparePublishedCreation(ctx, e); err != nil {
		return err
	}
	x := p.c.sources
	current := storageOwnerCatalogV2{OriginalOwner: x.owner, Objects: x.objects, OwnerControls: x.controls}
	if storageEnrollmentDefaultControlAppendEqual(current, e.Inner.Catalog, &e.Board.Source) {
		return x.installCompletedHistoryDictionary(ctx, e.Inner.Catalog, e.Board.Source)
	}
	// Fresh startup discovery can already observe the exact desired relation.
	// Require every desired original object/native/parent/owner/bound plus all
	// complete control/registry material before copying its exact dictionary.
	if err := p.compareOriginalObjectDescriptions(ctx, e.Inner.Catalog); err != nil {
		return err
	}
	if err := x.restoreCreationDictionary(e.Inner.Catalog, nil); err != nil {
		return err
	}
	return x.current(ctx)
}

func (m *storageCreationLive) enrollRetiredHistory(ctx context.Context) error {
	if err := m.phase.boundary("creation-retired-before-enrollment"); err != nil {
		return err
	}
	if err := m.captureRetiredOriginal(ctx); err != nil {
		return err
	}
	catalog, source, err := m.planCreatedHistoryCatalog(ctx)
	if err != nil {
		return err
	}
	p := m.phase
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

// Family6 reconstructs a single child append from the full original retired
// page and the exact unchanged parent graph. It issues no native/Q permission.
func (p *storageEnrollmentEpoch) planPrivateCatalogOriginal(ctx context.Context, record storageCreationIntent, page []byte, retained *storageEnrollmentPeakSource) (storageOwnerCatalogV2, storageEnrollmentPeakSource, error) {
	var catalog storageOwnerCatalogV2
	var source storageEnrollmentPeakSource
	if p == nil || p.previousEvidence == nil || p.original == nil || p.initialInspection == nil || p.qualified == nil || p.qualified.record.Version < 2 || record.Kind != storageCreationPrivate || record.Branch != storageCreationContained || record.Core.Phase != storageIntentRetired || record.Core.Outcome != storageIntentPublished || record.Tail.Installation != p.c.ref.cell.ownerRecord.Installation || record.Tail.QBinding != p.c.ref.cell.binding || record.Tail.Project != p.original.Project || record.Tail.Config != p.c.ref.cell.ownerRecord.Config || record.Tail.Anchor != p.c.ref.cell.ownerRecord.Anchor || record.Tail.Qualification != p.qualified.digest || !storageCreationRetiredQCurrent(record, *p.initialInspection, p.previousEvidence.Inner.Catalog) {
		return catalog, source, storagebudget.ErrAuthority
	}
	encoded, err := storageEncodeCreationIntent(record, record.Tail.Installation, record.Tail.QBinding)
	if err != nil || !bytes.Equal(encoded, page) {
		return catalog, source, errStorageCustody
	}
	for i, raw := range p.original.Catalogs {
		if sha256.Sum256(raw) != record.Tail.Catalogs[i] {
			return catalog, source, errStorageCustody
		}
	}
	if err = p.held(ctx); err != nil {
		return catalog, source, err
	}
	before := p.previousEvidence.Inner.Catalog
	parents, err := storageCreationOriginalParentRows(record, before)
	if err != nil || len(parents) != 1 || len(record.Parents) != 1 || record.Parents[0].Role != 1 || record.Parents[0].Native != p.original.Project || parents[0].Role != 7 || parents[0].Source.Kind != 7 {
		return catalog, source, errStorageCustody
	}
	parent := parents[0]
	source, err = storageEnrollmentPeakDecodeSource(parent.Source.Relative)
	if err != nil || source.Default != nil || source.ActionPageDigest != (storagebudget.Digest{}) || source.Project != p.original.Project || !storageEnrollmentPeakSourceJoins(source, before, parent) {
		return catalog, source, errStorageCustody
	}
	child := storageOwnerPersistentObject{}
	for _, effect := range record.Effects {
		if effect.State != storageCreationDurable {
			return catalog, source, errStorageCustody
		}
		if effect.Parent == 0 && effect.Kind == 1 && effect.Name == ".desk-private" {
			if child != (storageOwnerPersistentObject{}) {
				return catalog, source, errStorageCustody
			}
			child = effect.Native
		}
	}
	if !child.valid(1) || child == source.Project || !storageIdentitySameVolume(child, source.Project) {
		return catalog, source, errStorageCustody
	}
	raw, err := json.Marshal(before)
	if err != nil || storageEnrollmentPeakCatalogJSON(raw, &catalog) != nil {
		return catalog, source, storagebudget.ErrBounds
	}
	cores, bank, err := p.originalCorePair(p.original.Catalogs, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim)
	if err != nil || catalog.Sequence > ^uint64(0)-5 || p.initialInspection.View.Configuration.Revision > ^uint64(0)-3 {
		return catalog, source, storagebudget.ErrBounds
	}
	catalog.Sequence++
	catalog.Version, catalog.Phase, catalog.Transition = 1, "prepared", record.Tail.Bridge
	catalog.ExpectedQRevision = p.initialInspection.View.Configuration.Revision + 1
	copy(catalog.PreviousCatalogDigest[:], cores[bank][32:64])
	catalog.BeforeDefinitionIDs = storageOwnerDefinitionIDs{Roots: []storagebudget.ID{}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{}, Constraints: []storagebudget.ID{}}
	for _, row := range before.Roots {
		catalog.BeforeDefinitionIDs.Roots = append(catalog.BeforeDefinitionIDs.Roots, row.Definition.ID)
	}
	for _, row := range before.Workspaces {
		catalog.BeforeDefinitionIDs.Workspaces = append(catalog.BeforeDefinitionIDs.Workspaces, row.Definition.ID)
	}
	for _, row := range before.Filesystems {
		catalog.BeforeDefinitionIDs.Filesystems = append(catalog.BeforeDefinitionIDs.Filesystems, row.Definition.ID)
	}
	for _, row := range before.Constraints {
		catalog.BeforeDefinitionIDs.Constraints = append(catalog.BeforeDefinitionIDs.Constraints, row.Definition.ID)
	}
	for _, object := range catalog.Objects {
		if object.Persistent == child {
			return catalog, source, errStorageCustody
		}
	}
	row := parent
	row.Role = 6
	if catalog.OriginalOwner.Kind == 2 {
		row.Role = 3
	}
	row.DirectoryObject = uint32(len(catalog.Objects))
	row.DomainObject, row.WorkObject = storageOwnerNoObject, storageOwnerNoObject
	row.References = append([]storagebudget.ID{}, parent.References...)
	row.Membership.ExistingObjects = []uint32{}
	row.Source = storageOwnerRootSource{Kind: 8, OwnerKey: append([]byte{}, parent.Source.OwnerKey...), Path: []byte{}, ParentRoot: parent.Definition.ID, Relative: []byte(".desk-private"), ControlIndex: storageOwnerNoControl}
	row.Definition.ID = storageEnrollmentProjectDefinitionID("private-child", catalog.Installation, record.Tail.Bridge, child)
	row.Definition.Generation = 1
	row.Definition.Locator = "project-private-child:" + storageHexID(row.Definition.ID)
	catalog.Objects = append(catalog.Objects, storageOwnerObject{Role: 20, Source: 3, Parent: parent.DirectoryObject, Name: []byte(".desk-private"), Persistent: child, OwnerClass: 2, ModeClass: 1})
	row.Definition.Identity, err = storageEnrollmentPeakChildIdentity(catalog, row)
	if err != nil {
		return catalog, source, err
	}
	var fs storagebudget.Filesystem
	constraints := []storagebudget.CapacityConstraint{}
	for _, f := range catalog.Filesystems {
		if f.Definition.ID == row.Definition.Filesystem {
			fs = f.Definition
		}
	}
	for _, c := range catalog.Constraints {
		constraints = append(constraints, c.Definition)
	}
	row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(row.Definition), fs, constraints)
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
	source.ActionPageDigest = sha256.Sum256(page)
	if retained != nil {
		a, e1 := storageEnrollmentPeakEncodeSource(source)
		b, e2 := storageEnrollmentPeakEncodeSource(*retained)
		if e1 != nil || e2 != nil || !bytes.Equal(a, b) {
			return catalog, source, errStorageCustody
		}
	}
	if err = storageEnrollmentPeakValidateCatalog(catalog, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim); err != nil {
		return catalog, source, err
	}
	if !storageEnrollmentRetainsOriginal(before, catalog) || !storageCreationTailCatalog(record, catalog) {
		return catalog, source, errStorageCustody
	}
	return catalog, source, p.held(ctx)
}

// Transfer the actual O_EXCL creator's retained child root into the existing
// original-source lifetime. No reopen/name/byte observation can supply it.
func (m *storageCreationLive) transferPrivateSource(ctx context.Context, catalog storageOwnerCatalogV2) error {
	if m == nil || m.self != m || m.phase == nil || m.phase.creation != m || m.record.Kind != storageCreationPrivate || m.record.Core.Phase != storageIntentRetired || !storageCreationTailCatalog(m.record, catalog) {
		return storagebudget.ErrAuthority
	}
	if err := m.current(ctx); err != nil {
		return err
	}
	x := m.phase.c.sources
	if x == nil || len(x.roots) >= 4096 {
		return storagebudget.ErrBounds
	}
	for i, effect := range m.record.Effects {
		if effect.Parent != 0 || effect.Kind != 1 || effect.Name != ".desk-private" {
			continue
		}
		object := &m.objects[i]
		if object.root == nil || object.source != nil || object.native != effect.Native {
			return errStorageCustody
		}
		if err := m.objectCurrent(ctx, i, true); err != nil {
			return err
		}
		var row *storageOwnerRoot
		for j := range catalog.Roots {
			candidate := &catalog.Roots[j]
			if candidate.Source.Kind == 8 && int(candidate.DirectoryObject) < len(catalog.Objects) && catalog.Objects[candidate.DirectoryObject].Persistent == object.native {
				if row != nil {
					return errStorageCustody
				}
				row = candidate
			}
		}
		if row == nil {
			return errStorageCustody
		}
		var parent *storageOriginalSourceRoot
		for _, candidate := range catalog.Roots {
			if candidate.Definition.ID == row.Source.ParentRoot {
				var err error
				parent, err = x.exactProjectScope(ctx, catalog, candidate)
				if err != nil {
					return err
				}
			}
		}
		if parent == nil || parent.root != object.parent {
			return errStorageCustody
		}
		for _, old := range x.roots {
			if old.persistent == object.native {
				return errStorageCustody
			}
		}
		edge := storageNamedRoot{parent: object.parent, name: object.name, root: object.root, identity: object.identity, private: true}
		if err := edge.current(); err != nil {
			return err
		}
		source := &storageOriginalSourceRoot{root: object.root, witnesses: []storageNamedRoot{edge}, path: filepath.Join(parent.path, ".desk-private"), key: string(row.Source.OwnerKey), role: row.Role, category: row.Category, persistent: object.native, parent: parent, relative: ".desk-private", workspaces: append([]storagebudget.ID{}, row.References...)}
		x.roots = append(x.roots, source)
		x.openedRoots = append(x.openedRoots, object.root)
		object.source = source
		return source.current(ctx, m.phase.c)
	}
	return errStorageCustody
}

func (m *storageCreationLive) enrollRetiredPrivate(ctx context.Context) error {
	if err := m.captureRetiredOriginal(ctx); err != nil {
		return err
	}
	p := m.phase
	catalog, source, err := p.planPrivateCatalogOriginal(ctx, m.record, m.selected.page, nil)
	if err != nil {
		return err
	}
	if err = m.transferPrivateSource(ctx, catalog); err != nil {
		return err
	}
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
