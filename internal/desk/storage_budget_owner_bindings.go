package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"reflect"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// This closed table names the concrete installed advisory routes. These hashes
// identify code contracts, not quota completeness or an allocation certificate.
// The matching native route must still query the actual normal held object.
func storageHostInstalledProfile(backend uint8) (storageOwnerProfile, error) {
	p := storageOwnerProfile{Version: 2, Backend: backend, AllowanceEvidence: storageAdvisoryPolicyEvidence()}
	var persistent, inodes, counter string
	switch backend {
	case 1:
		p.ID = 4097
		persistent = "linux-ext4-normal-fd-uuid-inode-generation-v2"
		inodes = "linux-statfs-finite-inodes-v2"
		counter = "linux-statfs-bavail-ext4-quota-discovery-v2"
	case 2:
		p.ID = 4098
		persistent = "darwin-normal-fd-volume-uuid-object-id-generation-revoke-v2"
		inodes = "apfs-documented-dynamic-inodes-v2"
		counter = "darwin-statfs-bavail-apfs-volume-restriction-discovery-v2"
	default:
		return p, storagebudget.ErrUnsupported
	}
	p.PersistentEvidence = sha256.Sum256([]byte(persistent))
	p.InodeEvidence = sha256.Sum256([]byte(inodes))
	p.CounterEvidence = sha256.Sum256([]byte(counter))
	var err error
	p.Evidence, err = p.digest()
	return p, err
}

func storageOwnerExactInstalledProfiles(profiles []storageOwnerProfile) error {
	if len(profiles) == 0 {
		return storagebudget.ErrUnsupported
	}
	for _, p := range profiles {
		want, err := storageHostInstalledProfile(p.Backend)
		if err != nil || p != want {
			return errors.Join(storagebudget.ErrUnsupported, err)
		}
	}
	return storageAdvisoryPlatformProfiles(profiles)
}

func storageOwnerDefinitionSnapshot(v storagebudget.HostInspection) storageOwnerDefinitions {
	c := v.Configuration
	out := storageOwnerDefinitions{Policy: storageOwnerPolicy{Installation: c.Policy.Installation, Workspace: c.Policy.Workspace, ObservationMaxAgeNanos: uint64(c.Policy.ObservationMaxAge)}, Roots: append([]storagebudget.RegisteredRoot{}, v.Roots...), Workspaces: append([]storagebudget.RegisteredWorkspace{}, v.Workspaces...), Filesystems: append([]storagebudget.Filesystem{}, c.Filesystems...), Constraints: append([]storagebudget.CapacityConstraint{}, c.CapacityConstraints...)}
	sort.Slice(out.Roots, func(i, j int) bool { return string(out.Roots[i].ID[:]) < string(out.Roots[j].ID[:]) })
	sort.Slice(out.Workspaces, func(i, j int) bool { return string(out.Workspaces[i].ID[:]) < string(out.Workspaces[j].ID[:]) })
	sort.Slice(out.Filesystems, func(i, j int) bool { return string(out.Filesystems[i].ID[:]) < string(out.Filesystems[j].ID[:]) })
	sort.Slice(out.Constraints, func(i, j int) bool { return string(out.Constraints[i].ID[:]) < string(out.Constraints[j].ID[:]) })
	return out
}

// Actual Q is inspected by the caller at one revision. This helper compares
// complete definitions; it never decides retirement from Terminal alone.
func storageOwnerActualDefinitions(v storagebudget.HostInspection, c storageOwnerCatalogV2) (before, after bool, err error) {
	d, err := storageOwnerDigest("desk-storage-owner-definitions-v2", storageOwnerDefinitionSnapshot(v))
	if err != nil {
		return false, false, err
	}
	return d == c.BeforeDefinitionsDigest, d == c.AfterDefinitionsDigest, nil
}

func storageOwnerRootValue(d storagebudget.RegisteredRoot) storagebudget.Root {
	return storagebudget.Root{ID: d.ID, Owner: d.Owner, Identity: d.Identity, Filesystem: d.Filesystem, Workspace: d.Workspace, Generation: d.Generation, Locator: d.Locator, CapacityCount: d.CapacityCount, CapacityRefs: d.CapacityRefs, CapacityBinding: d.CapacityBinding}
}

// Reattachment copies exact persisted rows. Live device/inode numbers are
// current-launch witnesses only and are never a source for durable IDs.
func (c *storageBudgetConstruction) attachOwnerBindings(ctx context.Context, cat storageOwnerCatalogV2, sources *storageOriginalSources) error {
	image, err := c.constructOwnerBindingImage(ctx, cat, sources)
	if err != nil {
		return err
	}
	return errors.Join(image.install(c.owner), image.Close())
}

func (c *storageBudgetConstruction) constructOwnerBindingImage(ctx context.Context, cat storageOwnerCatalogV2, sources *storageOriginalSources) (_ *storageOwnerBindingImage, err error) {
	o := c.owner
	image := newStorageOwnerBindingImage()
	defer func() {
		if err != nil {
			err = errors.Join(err, image.Close())
		}
	}()
	effective, err := c.effectiveOwnerDefinitions(ctx, cat)
	if err != nil {
		return nil, err
	}
	for _, w := range effective.Workspaces {
		image.workspaces[w.ID] = storagebudget.Workspace{ID: w.ID, Generation: w.Generation, Limits: w.Limits}
	}
	for _, f := range cat.Filesystems {
		image.filesystems[f.Definition.ID] = f.Definition
	}
	for _, v := range cat.Constraints {
		image.constraints[v.Definition.ID] = v.Definition
	}
	for _, row := range cat.Roots {
		src, err := sources.exactRoot(ctx, cat, row)
		if err != nil {
			return nil, err
		}
		dir, err := src.directoryIdentity()
		if err != nil {
			return nil, err
		}
		if prior := o.roots[row.Definition.ID]; prior != nil {
			markerChanged := false
			if src.marker == nil && row.Role == 4 {
				markerObject := (*storageOwnerObject)(nil)
				for i, object := range cat.Objects {
					if object.Role == 23 && object.Parent == row.DirectoryObject && string(object.Name) == dataMarkerName {
						if markerObject != nil {
							return nil, errStorageCustody
						}
						markerObject = &cat.Objects[i]
					}
				}
				if markerObject != nil {
					op := c.originalTransferOperation()
					if op == nil || op.record.Phase != storageTransferRetired || op.record.PointerState != 2 || op.record.MarkerState != 2 || markerObject.Persistent != op.record.MarkerNative || markerObject.FixedLength != op.record.MarkerBytes || storageTransferSelectionRelation(op.record, op.catalog, cat) != nil {
						return nil, errStorageCustody
					}
					target, e := storageTransferFinalTarget(op.record)
					if e != nil || src.persistent != target {
						return nil, errors.Join(errStorageCustody, e)
					}
					named, e := storageOpenFile(src.root, dataMarkerName, int64(markerObject.FixedLength))
					if e != nil {
						return nil, e
					}
					image.openedFiles = append(image.openedFiles, named.file)
					native, e := storageOwnerPersistentFile(ctx, named)
					if e != nil || native != op.record.MarkerNative {
						return nil, errors.Join(errStorageCustody, e)
					}
					raw := make([]byte, markerObject.FixedLength)
					n, e := named.file.ReadAt(raw, 0)
					if e != nil || n != len(raw) || sha256.Sum256(raw) != op.record.MarkerDigest {
						return nil, errors.Join(errStorageCustody, e)
					}
					named.immutable = raw
					if e = named.current(); e != nil {
						return nil, e
					}
					clone := *src
					clone.marker = &named
					if image.sourceAliases == nil {
						image.sourceAliases = map[*storageOriginalSourceRoot]*storageOriginalSourceRoot{}
					}
					image.sourceAliases[src] = &clone
					src = &clone
					markerChanged = true
				}
			}
			if storageRegisteredRoot(prior.row) != row.Definition || prior.root != src.root || prior.directory != dir || prior.category != row.Category {
				return nil, errStorageCustody
			}
			inactive := storageTransferCatalogView(row) && row.Source.ControlSelection == 4
			if reflect.DeepEqual(prior.references, row.References) && prior.transferInactive == inactive && !markerChanged {
				image.roots[row.Definition.ID] = prior
				continue
			}
			if prior.transferInactive != inactive {
				op := c.originalTransferOperation()
				if op == nil || prior.transferInactive || !inactive || op.record.Phase != storageTransferRejected && op.record.Phase != storageTransferRetired || op.retiredNamespace != row.Definition.ID || !bytes.Equal(op.retiredNamespaceSource, row.Source.Relative) || !reflect.DeepEqual(prior.references, row.References) {
					return nil, errStorageCustody
				}
				clone := *prior
				clone.self = &clone
				clone.transferInactive = true
				image.roots[row.Definition.ID] = &clone
				continue
			}
			// Only the original completed transfer may add the exact source cohort to
			// its existing target. All old native/domain/source descriptions remain
			// the same original aliases; ordinary constructors still refuse changes.
			op := c.originalTransferOperation()
			if op == nil || op.record.Phase != storageTransferRetired || op.record.PointerState != 2 || storageTransferSelectionRelation(op.record, op.catalog, cat) != nil {
				return nil, errStorageCustody
			}
			original, exists := storageTransferCatalogRoot(op.catalog, row.Definition.ID)
			if !exists || !storageTransferRootReferencesRetained(op.record, op.catalog, original, row) {
				return nil, errStorageCustody
			}
			clone := *prior
			clone.self = &clone
			clone.references = append([]storagebudget.ID{}, row.References...)
			clone.source = src
			image.roots[row.Definition.ID] = &clone
			continue
		}
		// The catalog requires an array even for an unreferenced anchor. Retain
		// that empty-array shape so an identical candidate reuses this binding
		// under the exact comparison above instead of refusing nil versus [].
		r := &storageRootBinding{owner: o, root: src.root, witnesses: src.witnesses, row: storageOwnerRootValue(row.Definition), directory: dir, category: row.Category, references: append([]storagebudget.ID{}, row.References...), configControl: row.Role == 1, qControl: row.Role == 2}
		r.transferInactive = storageTransferCatalogView(row) && row.Source.ControlSelection == 4
		r.self = r
		r.source = src
		if row.Source.Kind == 8 {
			r.scopeParent = row.Source.ParentRoot
		}
		if row.Role == 7 && row.Source.Kind == 7 || row.Role == 8 && row.Source.Kind == 9 {
			if src.scope == nil || row.Role == 7 && src.relative != ".desk-private" || row.Role == 8 && (src.scope.Default == nil || (src.scope.Default.Workflow != 2 && src.scope.Default.Workflow != 4)) {
				return nil, storagebudget.ErrAuthority
			}
			r.restrictedNamespace = src.relative
			// Derive the project domain from original adopted custody. A catalog
			// DomainObject is deliberately absent for this restricted source.
			f, e := src.root.Open(".")
			if e != nil {
				return nil, e
			}
			info, e := f.Stat()
			if e != nil {
				return nil, errors.Join(e, f.Close())
			}
			r.domainDirectory, r.domainName = true, "."
			r.domain = storageNamedFile{root: src.root, name: ".", file: f, identity: info}
			image.openedFiles = append(image.openedFiles, f)
		}
		if row.DomainObject != storageOwnerNoObject {
			domain := cat.Objects[row.DomainObject]
			// Every current private-data source shares the original installation
			// .data.lock. Provider/private directory turns remain their exact root.
			if row.DomainObject == row.DirectoryObject {
				f, e := src.root.Open(".")
				if e != nil {
					return nil, e
				}
				info, e := f.Stat()
				if e != nil {
					return nil, errors.Join(e, f.Close())
				}
				r.domainDirectory = true
				r.domainName = "."
				r.domain = storageNamedFile{root: src.root, name: ".", file: f, identity: info}
			} else {
				if !c.ownerBindingDomainMatches(ctx, cat, sources, domain) {
					return nil, storagebudget.ErrUnsupported
				}
				r.domainName = string(domain.Name)
				r.domain, err = storageOpenFile(c.original.config, r.domainName, 0)
				if err != nil {
					return nil, err
				}
			}
			image.openedFiles = append(image.openedFiles, r.domain.file)
		}
		image.roots[r.row.ID] = r
		if r.configControl {
			r.legacyReferences = append([]storagebudget.ID(nil), row.References...)
		}
	}
	// Kind8 has no serialized DomainObject. Borrow its actual original project
	// domain after all roots are assembled, preserving the same FD and alias.
	for _, r := range image.roots {
		if r.scopeParent == (storagebudget.ID{}) {
			continue
		}
		parent := image.roots[r.scopeParent]
		if parent == nil || parent == r || parent.source == nil || parent.source.role != 7 || parent.restrictedNamespace != ".desk-private" || parent.domain.file == nil || !parent.domainDirectory || r.source == nil || r.source.parent != parent.source {
			return nil, errStorageCustody
		}
		if r.domain.file != nil && r.domain.file != parent.domain.file {
			return nil, errStorageCustody
		}
		r.domain, r.domainName, r.domainDirectory = parent.domain, parent.domainName, true
	}
	return image, nil
}

func storageOwnerReadImmutable(ctx context.Context, root *os.Root, name string) ([]byte, storageNamedFile, error) {
	b, n, err := storageOwnerReadFixed(root, name, storageOwnerRecordBytes)
	if err != nil {
		return nil, n, err
	}
	if err = ctx.Err(); err != nil {
		return nil, storageNamedFile{}, errors.Join(err, n.file.Close())
	}
	n.immutable = append([]byte(nil), b...)
	return b, n, nil
}

// Domain indices belong to their own catalog dictionaries. Ordinary binding
// retains its original checks; only the registered transfer's exact closed
// selection may translate the config index after successor object sorting.
// This comparison grants no native effect or enrollment permission.
func (c *storageBudgetConstruction) ownerBindingDomainMatches(ctx context.Context, cat storageOwnerCatalogV2, sources *storageOriginalSources, domain storageOwnerObject) bool {
	if domain.Role != 18 || int(domain.Parent) >= len(cat.Objects) || int(sources.configObject) >= len(sources.objects) || cat.Objects[domain.Parent].Persistent != sources.objects[sources.configObject].Persistent {
		return false
	}
	if domain.Parent == sources.configObject || sources.admittedDefaultSource(cat) != nil || sources.admittedHistoryAncestorObjectIndices(ctx, cat) {
		return true
	}
	if c == nil || c.sources != sources || sources.construction != c {
		return false
	}
	op := c.originalTransferOperation()
	if op == nil || op.record.Phase != storageTransferRetired || op.record.PointerState != 2 {
		return false
	}
	original := storageOwnerCatalogV2{OriginalOwner: sources.owner, Objects: sources.objects, OwnerControls: sources.controls}
	return storageEnrollmentOriginalOwnerEqual(original, op.catalog, nil) && storageTransferSelectionRelation(op.record, op.catalog, cat) == nil
}
