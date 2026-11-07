package desk

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func (x *storageOriginalSources) buildObjects(ctx context.Context) error {
	c := x.construction
	appendObject := func(o storageOwnerObject, parent storageOwnerPersistentObject) {
		x.objects = append(x.objects, o)
		x.objectParents = append(x.objectParents, parent)
	}
	for _, s := range x.roots {
		role, source := uint8(20), uint8(3)
		ownerClass := uint8(2)
		switch s.role {
		case 1:
			role = 1
			source = 1
			ownerClass = 1
		case 2:
			role = 2
			source = 1
			ownerClass = 1
		case 4:
			role = 21
		case 5:
			role = 22
		}
		name := []byte{}
		parent := storageOwnerPersistentObject{}
		if s.parent != nil {
			source = 3
			ownerClass = 2
			if s.parent.role == 1 || s.parent.role == 2 {
				source = 1
				ownerClass = 1
			}
			parts := strings.Split(s.relative, "/")
			if len(parts) > 64 {
				return storagebudget.ErrBounds
			}
			root := s.parent.root
			parent = s.parent.persistent
			for _, part := range parts[:len(parts)-1] {
				n, err := storagePinChild(root, part, true)
				if err != nil {
					return err
				}
				x.openedRoots = append(x.openedRoots, n.root)
				s.witnesses = append(s.witnesses, n)
				p, err := storageOwnerPersistentRoot(ctx, n.root)
				if err != nil {
					return err
				}
				appendObject(storageOwnerObject{Role: 20, Source: source, Name: []byte(part), Persistent: p, OwnerClass: ownerClass, ModeClass: 1}, parent)
				root = n.root
				parent = p
			}
			name = []byte(parts[len(parts)-1])
			last, err := storagePinChild(root, string(name), true)
			if err != nil {
				return err
			}
			x.openedRoots = append(x.openedRoots, last.root)
			s.witnesses = append(s.witnesses, last)
			p, err := storageOwnerPersistentRoot(ctx, last.root)
			if err != nil || p != s.persistent {
				return errors.Join(errStorageCustody, err)
			}
		}
		appendObject(storageOwnerObject{Role: role, Source: source, Name: name, Persistent: s.persistent, OwnerClass: ownerClass, ModeClass: 1}, parent)
		if s.marker != nil {
			p, err := storageOwnerPersistentFile(ctx, *s.marker)
			if err != nil {
				return err
			}
			mr := uint8(23)
			if s.role == 5 {
				mr = 24
			}
			appendObject(storageOwnerObject{Role: mr, Source: source, Name: []byte(s.marker.name), Persistent: p, OwnerClass: ownerClass, ModeClass: 2, FixedLength: uint64(s.marker.size)}, s.persistent)
		}
	}
	for _, n := range c.original.files {
		role := uint8(0)
		for i, name := range storageOwnerRoleNames {
			if (i >= 4 && i <= 19 || i >= 25 && i <= 27) && name == n.name {
				role = uint8(i)
				break
			}
		}
		if role == 0 {
			continue
		}
		p, err := storageOwnerPersistentFile(ctx, n)
		if err != nil {
			return err
		}
		parent, err := storageOwnerPersistentRoot(ctx, n.root)
		if err != nil {
			return err
		}
		appendObject(storageOwnerObject{Role: role, Source: 1, Name: []byte(n.name), Persistent: p, OwnerClass: 1, ModeClass: 2, FixedLength: uint64(n.size)}, parent)
	}
	type pair struct {
		o storageOwnerObject
		p storageOwnerPersistentObject
	}
	rows := make([]pair, len(x.objects))
	for i, o := range x.objects {
		rows[i] = pair{o, x.objectParents[i]}
	}
	sort.Slice(rows, func(i, j int) bool { return storageOwnerPOCompare(rows[i].o.Persistent, rows[j].o.Persistent) < 0 })
	indices := map[storageOwnerPersistentObject]uint32{}
	for i, row := range rows {
		if _, ok := indices[row.o.Persistent]; ok {
			return storagebudget.ErrAuthority
		}
		indices[row.o.Persistent] = uint32(i)
	}
	for i, row := range rows {
		o := row.o
		o.Parent = storageOwnerNoObject
		if row.p != (storageOwnerPersistentObject{}) {
			p, ok := indices[row.p]
			if !ok {
				return storagebudget.ErrAuthority
			}
			o.Parent = p
		}
		x.objects[i] = o
		if o.Role == 1 {
			x.configObject = uint32(i)
		}
	}
	if x.owner.Kind == 2 {
		x.controls = append(x.controls, storageOwnerControl{Role: 1, ParentObject: x.configObject, Name: []byte(deskHomeRegistryName), OwnerKind: 1, OwnerKey: []byte(x.owner.HomeOwner), MaxBytes: 1048576, Location: storageOwnerLocation{Path: []byte{}, Previous: []byte{}}})
	}
	for i := range x.controls {
		v := &x.controls[i]
		if v.Role == 2 {
			matched := false
			for _, s := range x.roots {
				if storageHistoryControlSelection(*v, s) == 1 {
					parent := c.original.config
					if s.parent != nil {
						parent = s.parent.root
					}
					p, err := storageOwnerPersistentRoot(ctx, parent)
					if err != nil {
						return err
					}
					index, ok := indices[p]
					if !ok || matched {
						return storagebudget.ErrAuthority
					}
					v.ParentObject = index
					matched = true
				}
			}
			if !matched {
				return storagebudget.ErrAuthority
			}
		}
	}
	sort.Slice(x.controls, func(i, j int) bool {
		a, b := x.controls[i], x.controls[j]
		n := storageOwnerPOCompare(x.objects[a.ParentObject].Persistent, x.objects[b.ParentObject].Persistent)
		return n < 0 || n == 0 && string(a.Name) < string(b.Name)
	})
	for i, v := range x.controls {
		if v.Role == 1 {
			x.owner.RegistryControl = uint32(i)
		}
	}
	template := storageOwnerCatalogV2{Objects: x.objects}
	for i, v := range x.controls {
		d, err := template.controlDigest(v)
		if err != nil {
			return err
		}
		x.controls[i].RelationDigest = d
	}
	return nil
}

func (c *storageBudgetConstruction) constructOwnerCatalog(ctx context.Context, x *storageOriginalSources, r storageOwnerRecordV2, claim storageOwnerClaimV2, profile storageOwnerProfile, v storagebudget.HostInspection) (cat storageOwnerCatalogV2, err error) {
	p := v.Configuration.Policy
	cat = storageOwnerCatalogV2{Version: 3, Installation: r.Installation, QBinding: r.QBinding, Sequence: 1, Phase: "prepared", ExpectedQRevision: v.Configuration.Revision, Policy: storageOwnerPolicy{Installation: p.Installation, Workspace: p.Workspace, ObservationMaxAgeNanos: uint64(p.ObservationMaxAge)}, BeforeDefinitionIDs: storageOwnerDefinitionIDs{Roots: []storagebudget.ID{}, Workspaces: []storagebudget.ID{}, Filesystems: []storagebudget.ID{}, Constraints: []storagebudget.ID{}}, OriginalOwner: x.owner, Objects: x.objects, OwnerControls: x.controls, Roots: []storageOwnerRoot{}, Workspaces: x.workspaces, Filesystems: []storageOwnerFilesystem{}, Constraints: []storageOwnerConstraint{storageOwnerAdvisoryConstraint(r.Installation)}, Profiles: []storageOwnerProfile{profile}}
	cat.Transition, err = storagebudget.NewID()
	if err != nil {
		return cat, err
	}
	object := map[storageOwnerPersistentObject]uint32{}
	fixed := map[uint8]uint32{}
	for i, o := range cat.Objects {
		object[o.Persistent] = uint32(i)
		fixed[o.Role] = uint32(i)
	}
	rootIDs := map[*storageOriginalSourceRoot]storagebudget.ID{}
	for _, s := range x.roots {
		id, e := storagebudget.NewID()
		if e != nil {
			return cat, e
		}
		rootIDs[s] = id
	}
	fsByVolume := map[[16]byte]storageOwnerFilesystem{}
	for _, s := range x.roots {
		if s.persistent.Backend != profile.Backend {
			return cat, storagebudget.ErrUnsupported
		}
		fs, ok := fsByVolume[s.persistent.VolumeUUID]
		if !ok {
			id, e := storagebudget.NewID()
			if e != nil {
				return cat, e
			}
			identity, e := storageOwnerDigest("desk-storage-original-volume-v3", s.persistent.VolumeUUID)
			if e != nil {
				return cat, e
			}
			f := storagebudget.Filesystem{ID: id, Identity: identity, Generation: 1, CapabilityEvidence: profile.InodeEvidence, ByteConstraint: cat.Constraints[0].Definition.ID, InodeModel: storagebudget.InodesFinite, InodeConstraint: cat.Constraints[0].Definition.ID}
			if profile.Backend == 2 {
				f.InodeModel = storagebudget.InodesDynamic
				f.InodeConstraint = storagebudget.ID{}
			}
			fs = storageOwnerFilesystem{Definition: f, Backend: profile.Backend, VolumeUUID: s.persistent.VolumeUUID, PersistentProfile: profile.ID, InodeProfile: profile.ID}
			fsByVolume[s.persistent.VolumeUUID] = fs
			cat.Filesystems = append(cat.Filesystems, fs)
		}
		m := storageOwnerMembership{Backend: profile.Backend, ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileEvidence: profile.Evidence, Pool: fs.Definition.ID, VolumeUUID: s.persistent.VolumeUUID, Creation: 1, ExistingObjects: []uint32{}}
		if profile.Backend == 1 {
			m.User = storageOwnerSubject{State: 4, Kind: 1}
			m.Group = storageOwnerSubject{State: 4, Kind: 2}
			m.Project = storageOwnerSubject{State: 4, Kind: 3}
		}
		dir := object[s.persistent]
		for i, o := range cat.Objects {
			if o.ModeClass == 2 && o.Parent == dir {
				m.ExistingObjects = append(m.ExistingObjects, uint32(i))
			}
		}
		definition := storagebudget.RegisteredRoot{ID: rootIDs[s], Owner: r.QBinding, Filesystem: fs.Definition.ID, Generation: 1, Locator: "original:" + storageHexID(rootIDs[s]), CapacityCount: 1}
		definition.CapacityRefs[0] = storagebudget.CapacityRef{Constraint: cat.Constraints[0].Definition.ID, Generation: 1}
		refs := storageUniqueIDs(s.workspaces)
		// The closed catalog codec requires arrays, including an unreferenced anchor.
		if refs == nil {
			refs = []storagebudget.ID{}
		}
		if len(refs) == 1 && s.role != 1 {
			definition.Workspace = refs[0]
		}
		source := storageOwnerRootSource{Kind: s.role, OwnerKey: []byte(s.key), Path: []byte(s.path), Relative: []byte(s.relative), ControlIndex: storageOwnerNoControl}
		if s.parent != nil {
			source.ParentRoot = rootIDs[s.parent]
		}
		row := storageOwnerRoot{Definition: definition, Role: s.role, Category: s.category, References: refs, Source: source, DirectoryObject: dir, DomainObject: fixed[18], WorkObject: fixed[19], Membership: m}
		if s.role == 2 {
			row.DomainObject = storageOwnerNoObject
			row.WorkObject = storageOwnerNoObject
		}
		if s.role == 5 {
			if s.marker == nil {
				return cat, storagebudget.ErrAuthority
			}
			mp, e := storageOwnerPersistentFile(ctx, *s.marker)
			if e != nil {
				return cat, e
			}
			row.DomainObject = dir
			row.WorkObject = storageOwnerNoObject
			row.Source.ControlKind = 1
			row.Source.ControlIndex = object[mp]
		}
		if s.role == 4 {
			settings := r.Config
			if s.parent != nil {
				settings = s.parent.persistent
			}
			for i, v := range cat.OwnerControls {
				selection := storageHistoryControlSelection(v, s)
				if selection == 0 || int(v.ParentObject) >= len(cat.Objects) || cat.Objects[v.ParentObject].Persistent != settings {
					continue
				}
				if row.Source.ControlKind != 0 {
					return cat, storagebudget.ErrAuthority
				}
				row.Source.ControlKind = 2
				row.Source.ControlIndex = uint32(i)
				row.Source.ControlSelection = selection
			}
		}
		cat.Roots = append(cat.Roots, row)
	}
	for i, v := range cat.OwnerControls {
		cat.OwnerControls[i].RelationDigest, err = cat.controlDigest(v)
		if err != nil {
			return cat, err
		}
	}
	constraints := []storagebudget.CapacityConstraint{cat.Constraints[0].Definition}
	for i, row := range cat.Roots {
		row.Definition.Identity, err = cat.rootIdentity(row)
		if err != nil {
			return cat, err
		}
		row.Definition.CapacityBinding, err = storagebudget.CapacityBindingFor(storageOwnerRootValue(row.Definition), fsByVolume[row.Membership.VolumeUUID].Definition, constraints)
		if err != nil {
			return cat, err
		}
		cat.Roots[i] = row
	}
	sort.Slice(cat.Roots, func(i, j int) bool {
		return string(cat.Roots[i].Definition.ID[:]) < string(cat.Roots[j].Definition.ID[:])
	})
	sort.Slice(cat.Workspaces, func(i, j int) bool {
		return string(cat.Workspaces[i].Definition.ID[:]) < string(cat.Workspaces[j].Definition.ID[:])
	})
	sort.Slice(cat.Filesystems, func(i, j int) bool {
		return string(cat.Filesystems[i].Definition.ID[:]) < string(cat.Filesystems[j].Definition.ID[:])
	})
	b, a, err := cat.definitions()
	if err != nil {
		return cat, err
	}
	cat.BeforeDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", b)
	if err != nil {
		return cat, err
	}
	cat.AfterDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", a)
	if err != nil {
		return cat, err
	}
	return cat, cat.validateVersion(r, claim, 3)
}

type storageServerEnrollment struct {
	mu              sync.Mutex
	workspace       storagebudget.ID
	self            *storageServerEnrollment
	server          *Server
	construction    *storageBudgetConstruction
	ref             *storageInstallationRef
	closed          bool
	runtimePressure *storageRuntimePressureScope
}

func (c *storageBudgetConstruction) forServer(s *Server) (*storageServerEnrollment, error) {
	if s == nil || c == nil || c.self != c || c.owner == nil || c.ref == nil || s.deskOwner() != c.server || s.configDir != c.server.configDir || s.assistant == nil {
		return nil, storagebudget.ErrAuthority
	}
	c.mu.Lock()
	closing := c.closing || c.closed
	c.mu.Unlock()
	if closing {
		return nil, os.ErrClosed
	}
	if err := c.current(context.Background(), false); err != nil {
		return nil, err
	}
	if s != c.server {
		if !c.owner.admissionOpen() {
			return nil, storagebudget.ErrBusy
		}
		if s.cfg.parent != c.server || s.project == nil || s.project.own.root != s.root || c.server.home == nil || c.server.home.entry(s.projectDir) == nil {
			return nil, storagebudget.ErrAuthority
		}
		if s.retainedArtifactProviders != c.providers {
			return nil, storagebudget.ErrAuthority
		}
		if p, ok := s.retainedArtifacts.(*persistentInjectedArtifactProvider); ok {
			if c.sources == nil || c.providers == nil || c.providers.byID[p.id] == nil || c.providers.byID[p.id].provider != p || len(c.sources.selectedProviders[p.id]) == 0 {
				return nil, storagebudget.ErrAuthority
			}
		}
		parent := c.server.home.entry(s.projectDir)
		if c.sources == nil {
			return nil, storagebudget.ErrAuthority
		}
		original, ok := c.sources.projects[parent.ID]
		if !ok || original.entry != *parent {
			return nil, storagebudget.ErrAuthority
		}
		if e := original.named.current(); e != nil {
			return nil, e
		}
		a, e := original.named.root.Stat(".")
		b, be := s.root.Stat(".")
		if e != nil || be != nil || !os.SameFile(a, b) || !os.SameFile(b, s.project.info) {
			return nil, errors.Join(errStorageCustody, e, be)
		}

		// A child is an existing original home member. It cannot append a root,
		// initialize Q or mint a second installation by borrowing this wrapper.
	}
	ref, err := c.ref.borrow()
	if err != nil {
		return nil, err
	}
	workspace, err := c.storageStatusWorkspace(s)
	if err != nil {
		_ = ref.Close()
		return nil, err
	}
	// Recheck at publication, after the registry borrow/status lookup. A child
	// cannot publish a wrapper across a concurrent original-owner shutdown.
	if !c.owner.admissionOpen() {
		if s != c.server {
			return nil, errors.Join(storagebudget.ErrBusy, ref.Close())
		}
		if err := c.mutation.current(context.Background(), true); err != nil {
			return nil, errors.Join(err, ref.Close())
		}
	}
	e := &storageServerEnrollment{server: s, construction: c, ref: ref, workspace: workspace}
	e.self = e
	e.runtimePressure = &storageRuntimePressureScope{server: s, enrollment: e, owner: c.owner, construction: c}
	return e, nil
}
func (e *storageServerEnrollment) Close() error {
	if e == nil {
		return nil
	}
	if e.self != e {
		return storagebudget.ErrAuthority
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	(runtimeStoragePressureReporter{scope: e.runtimePressure, server: e.server, enrollment: e}).retire()
	return e.ref.Close()
}

func (s *Server) activateStorageBudgetStartup(ctx context.Context) (err error) {
	if !s.assistant.usable() {
		s.storageReadOnly = true
		s.storageProblem = s.assistant.problem
		s.recordStorageStatusProblem(s.assistant.problem)
		return nil
	}
	if s.cfg.parent != nil {
		c := s.cfg.parent.storageConstruction
		if c == nil {
			return storagebudget.ErrAuthority
		}
		e, err := c.forServer(s)
		if err != nil {
			return err
		}
		s.storageEnrollment = e
		return nil
	}
	c, err := s.prepareStorageBudgetConstruction(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil && c.mutation != nil {
			err = errors.Join(err, c.mutation.Close())
		}
		// The activation owns post-census identity/local completion and wrapper
		// setup too. Consume once, only after its original cleanup result.
		if c.owner != nil {
			c.owner.finishStartupMeasurement(c, ctx, err)
		}
	}()
	if err = c.loadOrEnrollOwner(ctx); err != nil {
		return err
	}
	c.sources.noteUnboundPrivate(c.owner)
	if err = s.recoverOriginalStorageMove(ctx); err != nil {
		return err
	}
	if err = s.storageInfrastructure.step("storage-startup", "owner-loaded"); err != nil {
		return err
	}
	if !c.startupIdentityCompleted {
		demand, e := c.originalIdentityDemand(ctx)
		if e != nil {
			return e
		}
		if demand {
			if e = c.completeOriginalIdentity(ctx); e != nil {
				return e
			}
			c.startupIdentityCompleted = true
		} else if c.startupComposed || c.startupLocal {
			if e = c.completeOriginalLocalCreation(ctx); e != nil {
				return e
			}
			c.startupIdentityCompleted = true
		}
	}
	if err = s.storageInfrastructure.step("storage-startup", "late-completion"); err != nil {
		return err
	}
	e, err := c.forServer(s)
	if err != nil {
		return err
	}
	s.storageEnrollment = e
	return nil
}

// Same-descriptor EX-to-SH handoff is followed by actual original-owner/Q and
// fresh numeric policy validation before recovery or write-capable workers.
func (s *Server) revalidateStorageBudgetStartup(ctx context.Context) (err error) {
	if s.storageEnrollment == nil {
		return nil
	}
	c := s.storageEnrollment.construction
	var handoff *storageMutationAuthority
	if s == c.server {
		handoff = c.handoff
		if handoff == nil {
			return storagebudget.ErrAuthority
		}
		defer func() {
			if err != nil {
				err = errors.Join(err, handoff.Close())
			}
		}()
		if err = handoff.current(ctx, false); err != nil {
			return err
		}
		ctx = storageMutationContext(ctx, handoff)
	} else if !c.owner.admissionOpen() {
		return storagebudget.ErrBusy
	}
	if err := c.current(ctx, false); err != nil {
		return err
	}
	if err := c.ref.cell.current(); err != nil {
		return err
	}
	v, err := c.ref.cell.q.InspectHost(ctx)
	if err != nil {
		return err
	}
	_, after, err := storageOwnerActualDefinitions(v, c.ref.cell.ownerCatalog)
	if err != nil || !after || v.Configuration.Maintenance {
		return errors.Join(errStorageOwnerRecovery, err)
	}
	snapshot, err := c.ref.cell.q.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err = storageOwnerUsageWithinLimits(snapshot.Installation, v.Configuration.Policy.Installation); err != nil {
		return err
	}
	for _, w := range snapshot.Workspaces {
		if err = storageOwnerUsageWithinLimits(w.Usage, w.Limits); err != nil {
			return err
		}
	}
	var pass *storageCapacityPass
	if handoff != nil {
		pass, err = c.owner.beginStorageCapacityPassForHandoff(ctx, handoff)
	} else {
		pass, err = c.owner.beginStorageCapacityPass(ctx, nil)
	}
	if err != nil {
		return err
	}
	if err = pass.Close(); err != nil {
		return err
	}
	if handoff != nil {
		// Only this actual successful original/Q/policy/pass-and-Close sequence
		// seals the ticket for the atomic compare/open. A ticket alone cannot open.
		g := &c.owner.admission
		g.mu.Lock()
		if g.phase == storageAdmissionHandoff && g.handoff == handoff && !handoff.closed && handoff.generation == g.generation {
			handoff.validatedPass = pass
		}
		g.mu.Unlock()
		return c.openAdmissionHandoff(ctx, handoff)
	}
	return nil
}

func storageOwnerUsageWithinLimits(usage [storagebudget.Categories]storagebudget.Vector, limits storagebudget.Limits) error {
	var total storagebudget.Vector
	for i, u := range usage {
		if !storageVectorLE(u, limits.Category[i]) {
			return storagebudget.ErrLimit
		}
		var err error
		total, err = storageVectorAdd(total, u)
		if err != nil {
			return err
		}
	}
	if !storageVectorLE(total, limits.Total) {
		return storagebudget.ErrLimit
	}
	return nil
}

// Private startup pressure/refusal state is never a saved permission bit.
// These existing file reads and the bounded metadata inspection remain usable;
// all other API/relay effects wait for original-owner reconciliation.
func (s *Server) serveStorageReadOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/session" || r.Method == http.MethodGet && (r.URL.Path == "/api/files" || r.URL.Path == "/api/file") {
		return false
	}
	if r.URL.Path == "/api/storage/status" && r.Method == http.MethodGet {
		s.handleStorageStatus(w, r)
		return true
	}
	if r.URL.Path == "/api/storage" && r.Method == http.MethodGet {
		if !s.guard(w, r) || s.refuseUnusableStore(w) {
			return true
		}
		w.Header().Set("Cache-Control", "no-store")
		// The legacy location/transfer DTO requires a separate safe settings read.
		// Do not substitute installation accounting or scan/create it in fallback.
		storageStatusRecovery(w)
		return true
	}

	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" || strings.HasPrefix(r.URL.Path, relayPrefix) || strings.HasPrefix(r.URL.Path, researchPrefix) {
		if !s.guard(w, r) {
			return true
		}
		// Storage is read-only where the private store is unusable. That is
		// the more specific refusal, worded as each route that keeps data
		// gives it (409, naming the directory to repair), not the generic
		// pause; the route is refused either way.
		if s.refuseUnusableStore(w) {
			return true
		}
		http.Error(w, "Storage budget is unavailable or exceeded; new work is paused.", http.StatusServiceUnavailable)
		return true
	}
	return false
}

// Only an actual original independent private root can demand automatic
// identity here. Copied/moved/unbound legacy identities keep their existing
// separate decision path. Absence remains lazy; it is never an empty Register.
func (c *storageBudgetConstruction) originalIdentityDemand(ctx context.Context) (bool, error) {
	if c == nil || c.self != c || c.sources == nil || c.owner == nil {
		return false, storagebudget.ErrAuthority
	}
	// Pending originals choose their retained project before considering the
	// current primary project's fresh identity demand.
	if c.home.exclusive {
		plan, local, err := c.pendingStartupOriginal(ctx)
		if err != nil {
			return false, err
		}
		if plan != nil {
			return true, nil
		}
		if local != nil {
			return false, nil
		}
	}
	if entry := c.server.homeEntry(); entry != nil && entry.Kind != "project" {
		return false, nil
	}
	if c.server.cfg.deskID != "" && c.server.homeEntry() == nil {
		return false, nil
	}
	project, err := storageOwnerPersistentRoot(ctx, c.project.own.root)
	if err != nil {
		return false, err
	}
	if c.startupLineage != nil {
		for _, plan := range c.startupLineage.Rows.LineagePlans {
			if plan.ProjectObject == project {
				return true, nil
			}
		}
	}
	var private *storageOriginalSourceRoot
	for _, source := range c.sources.roots {
		if source.path != filepath.Join(c.server.projectDir, startupIdentityDir) || source.category != storagebudget.Control || source.role != 3 && source.role != 6 {
			continue
		}
		if private != nil {
			return false, errStorageCustody
		}
		private = source
	}
	if private == nil || !c.owner.bindsSource(private) {
		// A private folder no registered root holds (one Desk made outside the
		// identity producer) cannot receive an identity through the producer;
		// it stays measured and reported (storageOriginalSources.unregistered).
		return false, nil
	}
	if err = private.current(ctx, c); err != nil {
		return false, err
	}
	record, _, found, err := readIdentity(private.root)
	if err != nil {
		return false, err
	}
	if found && (record.Path != c.server.projectDir || record.Folder == "" || record.Folder != c.server.folderKey()) {
		return false, nil
	}
	return true, nil
}
