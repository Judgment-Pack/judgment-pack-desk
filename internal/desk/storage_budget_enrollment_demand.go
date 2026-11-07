package desk

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// A child wrapper is only a caller identity. The original construction checks
// its complete retained registry edge and shared enrollment before issuing any
// native member; the wrapper itself never gains append or mutation authority.
func (c *storageBudgetConstruction) historyRequesterCurrent(ctx context.Context, s *Server) error {
	if c == nil || c.self != c || c.ref == nil || c.ref.cell == nil || s == nil || s.deskOwner() != c.server || s.storageEnrollment == nil {
		return storagebudget.ErrAuthority
	}
	e := s.storageEnrollment
	e.mu.Lock()
	valid := e.self == e && !e.closed && e.server == s && e.construction == c && e.ref != nil && e.ref.cell == c.ref.cell
	e.mu.Unlock()
	if !valid {
		return storagebudget.ErrAuthority
	}
	if err := c.originCurrent(ctx, false); err != nil {
		return err
	}
	if s == c.server {
		return nil
	}
	if s.cfg.parent != c.server || s.project == nil || s.project.own.root != s.root || c.server.home == nil || c.sources == nil {
		return storagebudget.ErrAuthority
	}
	if err := c.sources.currentProjects(ctx); err != nil {
		return err
	}
	entry := c.server.home.entry(s.projectDir)
	if entry == nil {
		return errStorageCustody
	}
	original, ok := c.sources.projects[entry.ID]
	if !ok || original.entry != *entry {
		return errStorageCustody
	}
	if err := original.named.current(); err != nil {
		return err
	}
	a, err := original.named.root.Stat(".")
	b, e0 := s.root.Stat(".")
	if err != nil || e0 != nil || !os.SameFile(a, b) || !os.SameFile(b, s.project.info) {
		return errors.Join(errStorageCustody, err, e0)
	}
	return nil
}
func (m *storageCreationLive) requesterCurrent(ctx context.Context) error {
	if m == nil || m.phase == nil || m.phase.c == nil {
		return storagebudget.ErrAuthority
	}
	if m.phase.startup && m.phase.startupProject != nil && m.recovering && m.record.Kind == storageCreationLocalPrivate && m.requester == nil {
		return m.phase.held(ctx)
	}
	if m.phase.startup && m.requester == m.phase.c.server {
		return m.phase.held(ctx)
	}
	return m.phase.c.historyRequesterCurrent(ctx, m.requester)
}

// Resolve settings from held originals, then verify the exact requested
// project's named private edge. Matching a stored path is never adoption.
func (c *storageBudgetConstruction) historySettings(ctx context.Context, s *Server) (*storageRootBinding, *storageOriginalHistorySelection, error) {
	if err := c.historyRequesterCurrent(ctx, s); err != nil {
		return nil, nil, err
	}
	return c.originalHistorySettings(ctx, s)
}

// Read-only source resolution shared with the original startup epoch. The live
// wrapper above first validates the real requester; startup already holds its
// original construction and complete domains and never recreates that wrapper.
func (c *storageBudgetConstruction) originalHistorySettings(ctx context.Context, s *Server) (*storageRootBinding, *storageOriginalHistorySelection, error) {
	if c == nil || s == nil || c.owner == nil || c.sources == nil {
		return nil, nil, storagebudget.ErrAuthority
	}
	var binding *storageRootBinding
	for _, r := range c.owner.orderedRoots() {
		if r.source == nil {
			continue
		}
		if !s.containedDeskData() {
			if r.configControl && r.root == c.original.config {
				binding = r
			}
			continue
		}
		if r.source.role != 3 && r.source.role != 6 || r.category != storagebudget.Control || r.source.path != filepath.Join(s.projectDir, ".desk-private") || len(r.source.witnesses) == 0 {
			continue
		}
		edge := r.source.witnesses[len(r.source.witnesses)-1]
		if edge.name != ".desk-private" || edge.root != r.root {
			return nil, nil, errStorageCustody
		}
		a, err := edge.parent.Stat(".")
		b, e := s.root.Stat(".")
		if err != nil || e != nil || !os.SameFile(a, b) {
			return nil, nil, errors.Join(errStorageCustody, err, e)
		}
		if binding != nil {
			return nil, nil, errStorageCustody
		}
		binding = r
	}
	if binding == nil {
		return nil, nil, errStorageOwnerTransitionRequired
	}
	if err := binding.current(ctx); err != nil {
		return nil, nil, err
	}
	var selection *storageOriginalHistorySelection
	for i := range c.sources.selections {
		v := &c.sources.selections[i]
		if v.settings == binding.root {
			if selection != nil {
				return nil, nil, errStorageCustody
			}
			selection = v
		}
	}
	if selection == nil || selection.pending != nil {
		return nil, nil, errStorageEnrollmentUnresolved
	}
	return binding, selection, nil
}

func (s *Server) ensureDefaultHistoryEnrollment(ctx context.Context) error {
	_, err := s.defaultHistoryEnrollmentTurn(ctx, false)
	return err
}

// Order first-use demands on the original server's existing transaction FIFO.
// A queued caller owns no retain, admission or native lock; after its predecessor
// drains it inspects the original current selection itself. Keep custody-owning
// callers try-only. The FIFO is released before any caller delivers a response.
func (s *Server) defaultHistoryEnrollmentTurn(ctx context.Context, tryOnly bool) (nonmatchingWait time.Duration, err error) {
	if s.storageEnrollment == nil {
		return nonmatchingWait, nil
	}
	c := s.storageEnrollment.construction
	if c == nil || c.self != c || c.owner == nil || c.server == nil {
		return nonmatchingWait, storagebudget.ErrAuthority
	}
	var release func()
	var turn *privateDataTurnWitness
	if tryOnly {
		release, _, err = c.server.privateDataTurns.tryAcquireOriginal(ctx)
	} else {
		// Only exact original construction/requester identities classify this
		// existing FIFO turn. Fresh authority and selection checks still follow.
		key := privateDataFirstUseKey{construction: c, requester: s}
		release, turn, err = c.server.privateDataTurns.acquireFirstUseOriginal(ctx, key)
	}
	if err != nil {
		return nonmatchingWait, err
	}
	defer release()
	if !tryOnly {
		// Promotion freezes the original witness's charged waiting time. Carry
		// only this scheduling operand into the caller's short transaction.
		c.server.privateDataTurns.mu.Lock()
		nonmatchingWait = turn.nonmatching
		c.server.privateDataTurns.mu.Unlock()
	}
	// Retain/currentness and original history resolution occur after the wait.
	// Drop the read retain before acquiring the unchanged original epoch.
	if err = c.owner.retain(); err != nil {
		return nonmatchingWait, err
	}
	needed := false
	inspectErr := func() error {
		if err := c.current(ctx, false); err != nil {
			return err
		}
		settings, selection, err := c.historySettings(ctx, s)
		if err != nil {
			return err
		}
		if selection.explicit {
			return nil
		}
		if selection.location.Previous != "" || selection.location.Path != s.defaultChatDataPath() {
			return errStorageCustody
		}
		if _, err = selection.settings.Lstat(dataLocationName); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(errStorageCustody, err)
		}
		entries, err := storageEntries(settings.root)
		if err != nil {
			return err
		}
		path := s.defaultChatDataPath()
		if !s.containedDeskData() && storageDefaultLegacyNames(entries) {
			path = s.configDir
		}
		if err = s.validateDemandedHistoryPath(path); err != nil {
			return err
		}
		needed = true
		return nil
	}()
	if err = errors.Join(inspectErr, c.owner.drop()); err != nil || !needed {
		return nonmatchingWait, err
	}
	return nonmatchingWait, c.enrollDemandedHistory(ctx, s)
}

func (c *storageBudgetConstruction) enrollDemandedHistory(ctx context.Context, s *Server) (err error) {
	c.ref.cell.mu.Lock()
	existing := c.ref.cell.enrollmentEndpoint != nil
	c.ref.cell.mu.Unlock()
	if !existing {
		if err = c.enrollOriginalProjectParent(ctx); err != nil {
			return err
		}
	}
	if !s.containedDeskData() && s.cfg.DeskConfigDir == "" {
		if err = c.enrollDefaultHistoryAncestors(ctx, s); err != nil {
			return err
		}
	}
	p, err := c.beginEnrollmentEpoch(ctx)
	if err != nil {
		return err
	}
	defer func() { err = p.finish(err) }()
	if err = p.acquire(ctx); err != nil {
		return err
	}
	if err = p.captureExistingOriginal(ctx); err != nil {
		return err
	}
	if err = p.qualifyOriginalNative(ctx); err != nil {
		return err
	}
	proposed, payloads, err := p.planDemandedHistory(ctx, s)
	if err != nil || proposed == nil {
		return err
	}
	member, err := p.issueOriginalCreationFor(ctx, s, *proposed, payloads)
	if err != nil {
		return err
	}
	if err = member.execute(ctx); err != nil {
		return err
	}
	return member.enrollRetiredHistory(ctx)
}

// Derive a complete canonical component group from the actual original root
// and every referenced workspace. Physical allocation is charged once; the
// attribution-only rows retain the full original identities/capacity mapping.
func (p *storageEnrollmentEpoch) creationComponentGroup(ctx context.Context, r *storageIntentRecord, binding *storageRootBinding, category byte, amount storagebudget.Vector, effects uint64) (byte, error) {
	if err := p.held(ctx); err != nil {
		return 0, err
	}
	if binding == nil || binding.owner != p.c.owner || effects == 0 || effects > 4 {
		return 0, storagebudget.ErrAuthority
	}
	if err := binding.current(ctx); err != nil {
		return 0, err
	}
	observer, ok := p.c.owner.observer.(*storageAdvisoryObserver)
	if !ok {
		return 0, storagebudget.ErrAuthority
	}
	sample, err := observer.query(ctx, binding, binding.root)
	if err != nil {
		return 0, err
	}
	if err = p.c.owner.validateStorageSample(sample); err != nil {
		return 0, err
	}
	if sample.Filesystem != p.c.owner.filesystems[binding.row.Filesystem] || sample.RootGeneration != binding.row.Generation || sample.CapacityCount != binding.row.CapacityCount || sample.CapacityRefs != binding.row.CapacityRefs {
		return 0, errStorageCustody
	}
	// Reuse the accepted finite policy estimator for the actual fixed effects.
	// Each create/persist/close/readback group carries the existing28 metadata
	// allowance. This is a reservation operand, not physical qualification.
	peak, err := storageAdvisoryEstimate(amount.Bytes, amount.Files, amount.Directories, 28*effects)
	if err != nil {
		return 0, err
	}
	refs := binding.references
	if binding.configControl && category == storagebudget.Control {
		refs = nil
	}
	workspaces := append([]storagebudget.ID{}, refs...)
	if len(workspaces) == 0 {
		workspaces = []storagebudget.ID{{}}
	}
	if len(r.Components)+len(workspaces) > storagebudget.MaxComponents {
		return 0, storagebudget.ErrBounds
	}
	first := byte(len(r.Components))
	for i, id := range workspaces {
		physical := storagebudget.Vector{}
		if i == 0 {
			physical = amount
		}
		attribution := storagebudget.Vector{}
		if id != (storagebudget.ID{}) {
			attribution = amount
		}
		component := storageOwnerComponent(binding, id, category, physical, attribution, storagebudget.Vector{})
		if i == 0 {
			for j, constraint := range sample.Constraints {
				if constraint != p.c.owner.constraints[constraint.ID] {
					return 0, errStorageCustody
				}
				if constraint.Resources&storagebudget.CapacityBytes != 0 {
					component.Capacity[j].Peak.Bytes = peak.Bytes
				}
				if constraint.Resources&storagebudget.CapacityInodes != 0 {
					component.Capacity[j].Peak.Inodes = peak.Inodes
				}
			}
		}
		r.Components = append(r.Components, component)
		r.RootIdentities = append(r.RootIdentities, binding.row.Identity)
		r.FilesystemIdentities = append(r.FilesystemIdentities, sample.Filesystem.Identity)
		r.Locators = append(r.Locators, binding.row.Locator)
		r.Changes = append(r.Changes, storagebudget.Change{})
	}
	return first, p.held(ctx)
}

func (p *storageEnrollmentEpoch) planDemandedHistory(ctx context.Context, s *Server) (*storageCreationIntent, map[int][]byte, error) {
	if err := p.held(ctx); err != nil {
		return nil, nil, err
	}
	settings, selection, err := p.c.historySettings(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	if selection.explicit {
		return nil, nil, nil
	}
	if selection.location.Path != s.defaultChatDataPath() || selection.location.Previous != "" {
		return nil, nil, errStorageCustody
	}
	entries, err := storageEntries(settings.root)
	if err != nil {
		return nil, nil, err
	}
	branch := byte(storageCreationConfigured)
	target := "data"
	if s.containedDeskData() {
		branch = storageCreationContained
	} else if s.cfg.DeskConfigDir == "" {
		branch = storageCreationPersonal
		target = "history"
	}
	legacy := !s.containedDeskData() && storageDefaultLegacyNames(entries)
	path := s.defaultChatDataPath()
	if legacy {
		path = s.configDir
	}
	if err = s.validateDemandedHistoryPath(path); err != nil {
		return nil, nil, err
	}
	record := &storageCreationIntent{Kind: storageCreationHistory, Branch: branch, SelectedPath: path, Core: storageIntentRecord{Target: target, Stage: target, Workflow: "desk-monotone-private-creation-v1"}}
	location := s.encodeDataLocation(dataLocation{Version: 1, Path: path})
	if len(location) > 16384 {
		return nil, nil, storagebudget.ErrBounds
	}
	payloads := map[int][]byte{}
	var selected, parent *storageRootBinding
	for _, root := range p.originalRoots {
		if root.source == nil {
			continue
		}
		if root.source.path == path && (root.source.role == 4 || legacy && root.configControl) {
			if selected != nil {
				return nil, nil, errStorageCustody
			}
			selected = root
		}
		if !root.transferInactive && root.source.path == filepath.Dir(path) && (!legacy) && (branch != storageCreationPersonal || root.source.role == 8) {
			if parent != nil {
				return nil, nil, errStorageCustody
			}
			parent = root
		}
	}
	if selected != nil {
		if selected.source.persistent == (storageOwnerPersistentObject{}) || !legacy && selected.source.marker == nil {
			return nil, nil, errStorageCustody
		}
		record.Parents = []storageCreationParent{{Native: selected.source.persistent, Role: 6, Component: 255}, {Native: settings.source.persistent, Role: 5}}
		component, err := p.creationComponentGroup(ctx, &record.Core, settings, storagebudget.Control, storagebudget.Vector{Bytes: uint64(len(location)), Files: 1}, 1)
		if err != nil {
			return nil, nil, err
		}
		record.Parents[1].Component = component
		record.Effects = []storageCreationEffect{{Parent: 1, Kind: 2, Name: dataLocationName, Bytes: uint64(len(location)), Payload: sha256.Sum256(location)}}
		payloads[0] = location
	} else {
		if legacy || parent == nil {
			return nil, nil, errStorageOwnerTransitionRequired
		}
		if _, err = parent.root.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return nil, nil, errors.Join(errStorageCustody, err)
		}
		marker, err := json.Marshal(dataMarker{Version: 1, Owner: s.storageOwner()})
		if err != nil {
			return nil, nil, err
		}
		if len(marker) > 4096 {
			return nil, nil, storagebudget.ErrBounds
		}
		historyComponent, err := p.creationComponentGroup(ctx, &record.Core, parent, storagebudget.History, storagebudget.Vector{Bytes: uint64(len(marker)), Files: 1, Directories: 1}, 2)
		if err != nil {
			return nil, nil, err
		}
		controlComponent, err := p.creationComponentGroup(ctx, &record.Core, settings, storagebudget.Control, storagebudget.Vector{Bytes: uint64(len(location)), Files: 1}, 1)
		if err != nil {
			return nil, nil, err
		}
		record.Parents = []storageCreationParent{{Native: parent.source.persistent, Role: 4, Component: historyComponent}, {Native: settings.source.persistent, Role: 5, Component: controlComponent}}
		record.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: target}, {Parent: 2, Kind: 2, Name: dataMarkerName, Bytes: uint64(len(marker)), Payload: sha256.Sum256(marker)}, {Parent: 1, Kind: 2, Name: dataLocationName, Bytes: uint64(len(location)), Payload: sha256.Sum256(location)}}
		payloads[1] = marker
		payloads[2] = location
	}
	for _, effect := range record.Effects {
		record.Core.NewLength += effect.Bytes
	}
	return record, payloads, p.held(ctx)
}

// Apply the same existing machine/path policy as openChatDataAt before the
// original enrollment member can make any default namespace effect.
func (s *Server) validateDemandedHistoryPath(path string) error {
	if !storageOwnerAbsolute([]byte(path)) || filepath.Clean(path) != path {
		return errStorageCustody
	}
	if err := s.policyMachineTarget(path); err != nil {
		return err
	}
	if s.containedDeskData() && !pathContains(filepath.Join(s.projectDir, ".desk-private"), path) {
		return errStorageCustody
	}
	if !s.containedDeskData() && path != s.configDir && pathContains(s.projectDir, path) {
		return errStorageCustody
	}
	return nil
}

// Draft demand enters the original owner before acquiring the workflow lock.
// Inspection only selects retained bindings; original enrollment rechecks the
// actual requester, names, complete reference set and qualified native parents.
func (c *storageBudgetConstruction) originalDraftBindings(ctx context.Context, s *Server) (private, parent *storageRootBinding, err error) {
	if err = c.historyRequesterCurrent(ctx, s); err != nil {
		return nil, nil, err
	}
	project, err := storageOwnerPersistentRoot(ctx, s.root)
	if err != nil {
		return nil, nil, err
	}
	for _, binding := range c.owner.orderedRoots() {
		source := binding.source
		if source == nil || binding.category != storagebudget.Control {
			continue
		}
		if source.role == 7 && source.scope != nil && source.scope.Project == project {
			if parent != nil {
				return nil, nil, errStorageCustody
			}
			parent = binding
		}
		if (source.role == 3 || source.role == 6) && source.path == filepath.Join(s.projectDir, startupIdentityDir) {
			if private != nil || len(source.witnesses) == 0 {
				return nil, nil, errStorageCustody
			}
			edge := source.witnesses[len(source.witnesses)-1]
			a, e := edge.parent.Stat(".")
			b, e0 := s.root.Stat(".")
			if e != nil || e0 != nil || edge.name != startupIdentityDir || edge.root != binding.root || !os.SameFile(a, b) {
				return nil, nil, errors.Join(errStorageCustody, e, e0)
			}
			private = binding
		}
	}
	for _, binding := range []*storageRootBinding{private, parent} {
		if binding != nil {
			if err = binding.current(ctx); err != nil {
				return nil, nil, err
			}
		}
	}
	if private == nil {
		if _, e := s.root.Lstat(startupIdentityDir); e == nil {
			return nil, nil, errStoragePrivateUnregistered
		} else if !errors.Is(e, os.ErrNotExist) {
			return nil, nil, errors.Join(errStorageOwnerTransitionRequired, e)
		}
	}
	return private, parent, nil
}

// requireRegisteredPrivate refuses, in words, a project whose existing private
// folder no registered root holds (errStoragePrivateUnregistered).
func (s *Server) requireRegisteredPrivate(ctx context.Context) error {
	if s.storageEnrollment == nil {
		return nil
	}
	c := s.storageEnrollment.construction
	if c == nil || c.owner == nil {
		return storagebudget.ErrAuthority
	}
	if err := c.owner.retain(); err != nil {
		return err
	}
	_, _, err := c.originalDraftBindings(ctx, s)
	return errors.Join(err, c.owner.drop())
}

// errStoragePrivateUnregistered refuses work that needs a project's private
// folder registered with storage (dialect drafts, the identity a project move
// carries) when the folder exists but no registered root holds it: Desk made
// it outside the identity producer. Storage measures and reports it and Desk
// keeps working; registering an existing folder is later work (desk #698),
// because the producer's creation path never adopts a folder it did not make.
// It is the owner transition refusal, in words.
var errStoragePrivateUnregistered error = storagePrivateUnregisteredError{}

type storagePrivateUnregisteredError struct{}

func (storagePrivateUnregisteredError) Error() string {
	return "This project's .desk-private folder was made before storage registered it. Desk keeps and counts its data, but cannot keep dialect drafts in it or move this project until a later Desk release registers it. Storage in Admin lists the folder."
}
func (storagePrivateUnregisteredError) Is(target error) bool {
	return target == errStorageOwnerTransitionRequired
}
func storageDraftMissing(root *os.Root) (directory, ignore, workflow bool, err error) {
	edge, e := storagePinChild(root, "dialect-drafts", true)
	if e != nil {
		// Pinning deliberately collapses missing and unsafe names to a custody
		// refusal. Prove absence on this exact retained parent, as discovery
		// does; never interpret an unsafe existing child as missing.
		if _, le := root.Lstat("dialect-drafts"); errors.Is(le, os.ErrNotExist) {
			return true, true, true, nil
		}
		return false, false, false, e
	}
	defer func() { err = errors.Join(err, edge.current(), edge.root.Close()) }()
	for _, name := range []string{".gitignore", "workflow.lock"} {
		info, e := edge.root.Lstat(name)
		if errors.Is(e, os.ErrNotExist) {
			if name == ".gitignore" {
				ignore = true
			} else {
				workflow = true
			}
			continue
		}
		if e != nil || !storagePrivate(info, false) || name == "workflow.lock" && info.Size() != 0 {
			return false, false, false, errors.Join(errStorageCustody, e)
		}
		n, e := storageOpenFile(edge.root, name, info.Size())
		if e != nil {
			return false, false, false, e
		}
		if e = errors.Join(n.current(), n.file.Close()); e != nil {
			return false, false, false, e
		}
	}
	return false, ignore, workflow, nil
}
func (s *Server) ensureDialectDraftEnrollment(ctx context.Context) (err error) {
	if s.storageReadOnly {
		return errStorageEnrollmentUnresolved
	}
	if s.storageEnrollment == nil {
		return nil
	}
	c := s.storageEnrollment.construction
	if c == nil || c.owner == nil {
		return storagebudget.ErrAuthority
	}
	if err = c.owner.retain(); err != nil {
		return err
	}
	needed := false
	inspectErr := func() error {
		if err := c.current(ctx, false); err != nil {
			return err
		}
		private, _, err := c.originalDraftBindings(ctx, s)
		if err != nil {
			return err
		}
		if private == nil {
			needed = true
			return nil
		}
		directory, ignore, workflow, err := storageDraftMissing(private.root)
		needed = directory || ignore || workflow
		return err
	}()
	if err = errors.Join(inspectErr, c.owner.drop()); err != nil || !needed {
		return err
	}
	return c.enrollDemandedDraft(ctx, s)
}
func (c *storageBudgetConstruction) enrollDemandedDraft(ctx context.Context, s *Server) error {
	private, parent, err := func() (*storageRootBinding, *storageRootBinding, error) {
		if err := c.owner.retain(); err != nil {
			return nil, nil, err
		}
		a, b, e := c.originalDraftBindings(ctx, s)
		return a, b, errors.Join(e, c.owner.drop())
	}()
	if err != nil {
		return err
	}
	if private == nil && parent == nil {
		if err = c.enrollOriginalProjectParentFor(ctx, s); err != nil {
			return err
		}
	}
	createdPrivate := false
	err = func() (err error) {
		p, err := c.beginEnrollmentEpoch(ctx)
		if err != nil {
			return err
		}
		defer func() { err = p.finish(err) }()
		if err = p.selectOriginalRequester(ctx, s); err != nil {
			return err
		}
		if err = p.acquire(ctx); err != nil {
			return err
		}
		c.ref.cell.mu.Lock()
		existing := c.ref.cell.enrollmentEndpoint != nil
		c.ref.cell.mu.Unlock()
		if existing {
			err = p.captureExistingOriginal(ctx)
		} else {
			err = p.captureInitialOriginal(ctx)
		}
		if err != nil {
			return err
		}
		if err = p.qualifyOriginalNative(ctx); err != nil {
			return err
		}
		proposed, payloads, err := p.planDemandedDraft(ctx, s)
		if err != nil || proposed == nil {
			return err
		}
		m, err := p.issueOriginalCreationFor(ctx, s, *proposed, payloads)
		if err != nil {
			return err
		}
		if err = m.execute(ctx); err != nil {
			return err
		}
		if m.record.Kind == storageCreationLocalPrivate {
			return nil
		}
		if err = m.enrollRetiredPrivate(ctx); err != nil {
			return err
		}
		createdPrivate = true
		return nil
	}()
	if err != nil {
		return err
	}
	if createdPrivate {
		automatic := s.cfg.deskID == ""
		if entry := s.homeEntry(); entry != nil {
			automatic = entry.Kind == "project"
		}
		if automatic {
			if err = c.completeOriginalIdentityFor(ctx, s); err != nil {
				return err
			}
			s.resolveStartupIdentity()
		}
	}
	return nil
}
func (p *storageEnrollmentEpoch) planDemandedDraft(ctx context.Context, s *Server) (*storageCreationIntent, map[int][]byte, error) {
	if err := p.held(ctx); err != nil {
		return nil, nil, err
	}
	private, parent, err := p.c.originalDraftBindings(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	r := &storageCreationIntent{Branch: storageCreationContained, SelectedPath: dialectDraftStoragePath(s.projectDir), Core: storageIntentRecord{Workflow: "desk-monotone-private-creation-v1"}}
	payloads := map[int][]byte{}
	binding := private
	if private == nil {
		if parent == nil || parent.source == nil || parent.source.scope == nil || parent.source.scope.Project != p.original.Project {
			return nil, nil, errStorageCustody
		}
		r.Kind = storageCreationPrivate
		r.Core.Target = startupIdentityDir
		r.Core.Stage = startupIdentityDir
		binding = parent
		r.Parents = []storageCreationParent{{Native: parent.source.persistent, Role: 1}}
		r.Effects = []storageCreationEffect{{Parent: 0, Kind: 1, Name: startupIdentityDir}, {Parent: 1, Kind: 1, Name: "dialect-drafts"}, {Parent: 2, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte("*\n"))}, {Parent: 2, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)}}
	} else {
		directory, ignore, workflow, e := storageDraftMissing(private.root)
		if e != nil {
			return nil, nil, e
		}
		if !directory && !ignore && !workflow {
			return nil, nil, nil
		}
		r.Kind = storageCreationLocalPrivate
		r.Core.Target = "dialect-drafts"
		r.Core.Stage = "dialect-drafts"
		r.Parents = []storageCreationParent{{Native: private.source.persistent, Role: 2}}
		effectParent := byte(1)
		if directory {
			r.Effects = append(r.Effects, storageCreationEffect{Parent: 0, Kind: 1, Name: "dialect-drafts"})
		} else {
			edge, e := storagePinChild(private.root, "dialect-drafts", true)
			if e != nil {
				return nil, nil, e
			}
			native, e := storageOwnerPersistentRoot(ctx, edge.root)
			e = errors.Join(e, edge.current(), edge.root.Close())
			if e != nil {
				return nil, nil, e
			}
			r.Parents = append(r.Parents, storageCreationParent{Native: native, Role: 3})
		}
		if ignore {
			r.Effects = append(r.Effects, storageCreationEffect{Parent: effectParent, Kind: 2, Name: ".gitignore", Bytes: 2, Payload: sha256.Sum256([]byte("*\n"))})
		}
		if workflow {
			r.Effects = append(r.Effects, storageCreationEffect{Parent: effectParent, Kind: 2, Name: "workflow.lock", Payload: sha256.Sum256(nil)})
		}
	}
	amount := storagebudget.Vector{}
	for i, e := range r.Effects {
		if e.Kind == 1 {
			amount.Directories++
		} else {
			amount.Bytes += e.Bytes
			amount.Files++
			r.Core.NewLength += e.Bytes
			if e.Name == ".gitignore" {
				payloads[i] = []byte("*\n")
			} else {
				payloads[i] = []byte{}
			}
		}
	}
	component, err := p.creationComponentGroup(ctx, &r.Core, binding, storagebudget.Control, amount, uint64(len(r.Effects)))
	if err != nil {
		return nil, nil, err
	}
	for i := range r.Parents {
		r.Parents[i].Component = component
	}
	return r, payloads, p.held(ctx)
}
