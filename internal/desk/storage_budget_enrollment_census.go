package desk

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func (p *storageEnrollmentEpoch) prepareEnrollmentCandidate(ctx context.Context, catalog storageOwnerCatalogV2) (err error) {
	if p == nil || p.self != p || p.scope == nil || p.candidate != nil || p.candidateObserver != nil || p.c.ref.cell.ownerRecord == nil {
		return storagebudget.ErrAuthority
	}
	if err = p.nativePermit(ctx); err != nil {
		return err
	}
	if err = storageEnrollmentPeakValidateCatalog(catalog, *p.c.ref.cell.ownerRecord, p.c.ref.cell.ownerClaim); err != nil {
		return err
	}
	if err = storageOwnerExactInstalledProfiles(catalog.Profiles); err != nil {
		return err
	}
	image, err := p.c.constructOwnerBindingImage(ctx, catalog, p.c.sources)
	if err != nil {
		return err
	}
	accepted := false
	defer func() {
		if !accepted {
			err = errors.Join(err, image.Close())
		}
	}()
	a := &storageAdvisoryObserver{owner: p.c.owner, catalog: catalog, roots: map[storagebudget.ID]storageOwnerRoot{}, filesystems: map[storagebudget.ID]storageOwnerFilesystem{}, constraints: map[storagebudget.ID]storageOwnerConstraint{}, maxAge: time.Duration(catalog.Policy.ObservationMaxAgeNanos)}
	for _, root := range catalog.Roots {
		a.roots[root.Definition.ID] = root
	}
	for _, fs := range catalog.Filesystems {
		a.filesystems[fs.Definition.ID] = fs
	}
	for _, constraint := range catalog.Constraints {
		a.constraints[constraint.Definition.ID] = constraint
	}
	// Independently check the actual original native mapping of every intended
	// row, including the selected project. This grants no native write and
	// installs neither definitions nor mutable ordinary owner state.
	for _, root := range image.roots {
		if err = root.current(ctx); err != nil {
			return err
		}
		sample, e := a.query(ctx, root, root.root)
		if e != nil {
			return e
		}
		if sample.Filesystem != image.filesystems[root.row.Filesystem] || sample.RootGeneration != root.row.Generation || sample.CapacityCount != root.row.CapacityCount || sample.CapacityRefs != root.row.CapacityRefs {
			return errStorageOwnerTransitionRequired
		}
		for _, row := range sample.Constraints {
			if row != image.constraints[row.ID] {
				return errStorageOwnerTransitionRequired
			}
		}
	}
	if err = p.held(ctx); err != nil {
		return err
	}
	p.candidate, p.candidateObserver = image, a
	accepted = true
	return nil
}

// Only an actual original walk can fill factualCensus. Decoded Section8 and
// caller-authored usage counters never fill it. Final extents are independently
// observed before and after the walk and remain fixed throughout this bridge.
func (p *storageEnrollmentEpoch) measureEnrollmentCandidate(ctx context.Context) (out storagebudget.Census, err error) {
	if p == nil || p.self != p || p.candidate == nil || p.candidate.installed || p.candidateObserver == nil || p.factualCensus != nil {
		return out, storagebudget.ErrAuthority
	}
	if err = p.nativePermit(ctx); err != nil {
		return out, err
	}
	for target := byte(1); target <= 8; target++ {
		if err = p.targetCurrent(ctx, target, true); err != nil {
			return out, err
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, storageCensusTime)
	defer cancel()
	walk := storageCensusWalk{owner: p.c.owner, ctx: queryCtx, roots: map[storageDirectoryIdentity]*storageRootBinding{}, seen: map[storageDirectoryIdentity]storageCensusSeen{}, directories: map[storageDirectoryIdentity]bool{}, attributed: map[storageCensusAttribution]bool{}, workspace: map[storagebudget.ID][storagebudget.Categories]storagebudget.Vector{}, readyMeasured: map[*storageRootBinding]bool{}, enrollment: p}
	roots := make([]*storageRootBinding, 0, len(p.candidate.roots))
	walk.namespaces = map[storageDirectoryIdentity]map[string]*storageRootBinding{}
	for _, root := range p.candidate.roots {
		roots = append(roots, root)
		if storageTransferNamespaceBinding(root) {
			retired, e := walk.originalTransferNamespaceRetired(root)
			if e != nil {
				return out, e
			}
			if retired {
				continue
			}
			if walk.namespaces[root.directory] == nil {
				walk.namespaces[root.directory] = map[string]*storageRootBinding{}
			}
			if walk.namespaces[root.directory][root.restrictedNamespace] != nil {
				return out, storagebudget.ErrAuthority
			}
			walk.namespaces[root.directory][root.restrictedNamespace] = root
		} else {
			if walk.roots[root.directory] != nil {
				return out, storagebudget.ErrAuthority
			}
			walk.roots[root.directory] = root
		}
	}
	if err = walk.captureOriginalTransferChains(roots); err != nil {
		return out, err
	}
	sort.Slice(roots, func(i, j int) bool {
		return strings.Compare(string(roots[i].row.ID[:]), string(roots[j].row.ID[:])) < 0
	})
	for id := range p.candidate.workspaces {
		walk.workspace[id] = [storagebudget.Categories]storagebudget.Vector{}
	}
	walk.observation = p.c.owner.beginOwnedMeasurement(roots, p.candidate.workspaces, p.candidate.filesystems)
	p.measurement = walk.observation
	for _, root := range roots {
		if err = p.nativePermit(queryCtx); err != nil {
			return storagebudget.Census{}, err
		}
		if err = root.current(queryCtx); err != nil {
			return storagebudget.Census{}, err
		}
		parent, e := root.censusControlParent()
		if e != nil {
			return storagebudget.Census{}, e
		}
		var usage [storagebudget.Categories]storagebudget.Vector
		count := 0
		retired, e := walk.originalTransferNamespaceRetired(root)
		if e != nil {
			return out, e
		}
		if !retired {
			if err = walk.directory(root, root.root, nil, "", 0, &count, &usage); err != nil {
				return storagebudget.Census{}, err
			}
		}
		if err = walk.carried(root, &count, &usage); err != nil {
			return storagebudget.Census{}, err
		}
		usage[storagebudget.Control], err = storageVectorAdd(usage[storagebudget.Control], parent.provision(walk.readyMeasured[root]))
		if err != nil {
			return storagebudget.Census{}, err
		}
		out.Roots = append(out.Roots, storagebudget.RootMeasurement{ID: root.row.ID, Generation: root.row.Generation, Usage: usage})
	}
	for id, usage := range walk.workspace {
		out.Workspaces = append(out.Workspaces, storagebudget.WorkspaceMeasurement{ID: id, Generation: p.candidate.workspaces[id].Generation, Usage: usage})
	}
	sort.Slice(out.Workspaces, func(i, j int) bool {
		return strings.Compare(string(out.Workspaces[i].ID[:]), string(out.Workspaces[j].ID[:])) < 0
	})
	raw, err := json.Marshal(struct {
		Roots      []storagebudget.RootMeasurement
		Workspaces []storagebudget.WorkspaceMeasurement
	}{out.Roots, out.Workspaces})
	if err != nil {
		return storagebudget.Census{}, err
	}
	out.Evidence = sha256.Sum256(append([]byte("desk-storage-complete-metadata-census-v1\x00"), raw...))
	for target := byte(1); target <= 8; target++ {
		if err = p.targetCurrent(ctx, target, true); err != nil {
			return storagebudget.Census{}, err
		}
	}
	if err = p.nativePermit(ctx); err != nil {
		return storagebudget.Census{}, err
	}
	retained := out
	retained.Roots = append([]storagebudget.RootMeasurement{}, out.Roots...)
	retained.Workspaces = append([]storagebudget.WorkspaceMeasurement{}, out.Workspaces...)
	p.factualCensus = &retained
	walk.observation.complete()
	return out, nil
}
