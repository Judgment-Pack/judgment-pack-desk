package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

// Pure selected metadata observation. Caller owns current writes/private data;
// final mutation rereads these exact rows under real marker/owner/work custody.
// The original v1 pathname is historical data and is never opened here.
func (s *Server) dialectArtifactProjectMoveSnapshot(ctx context.Context, previous, destination string, data *chatDataStore) (out *dialectArtifactProjectMovePlan, err error) {
	private, err := openDialectArtifactOwnerFolder(ctx, s.root, false, "move-project-private", ".desk-private")
	if injectedArtifactOnlyNotExist(err) {
		return nil, s.dialectArtifactMoveWithoutMarker(ctx, previous, data)
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "move-project-private", private.Close))
		if err != nil {
			out = nil
		}
	}()
	markerRaw, err := readDialectArtifactOwnerFile(private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	if injectedArtifactOnlyNotExist(err) {
		return nil, s.dialectArtifactMoveWithoutMarker(ctx, previous, data)
	}
	if err != nil {
		return nil, err
	}
	marker, err := readDialectArtifactOwnerMarker(markerRaw)
	if err != nil {
		return nil, err
	}
	m, err := s.openDialectArtifactSelectedMetadata(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, m.Close())
		if err != nil {
			out = nil
		}
	}()
	owner, err := m.ownerRow(ctx, marker.Owner)
	if err != nil {
		return nil, err
	}
	namespace, err := assistantRunNamespace(data.root, previous)
	if err != nil {
		return nil, err
	}
	scope, err := m.scopeRow(ctx, dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: marker.Owner, Namespace: namespace})
	if err != nil {
		return nil, err
	}
	p := &dialectArtifactProjectMovePlan{Version: 1, Owner: marker.Owner, LegacySHA256: digestOf(owner.raw), MarkerSHA256: digestOf(markerRaw), Namespace: namespace, NativeScopeSHA256: digestOf(scope.raw)}
	projectInfo, err := s.root.Stat(".")
	if err != nil {
		return nil, err
	}
	if native, decodeErr := readDialectArtifactOwnerRecord(owner.raw); decodeErr == nil {
		if native.FolderIdentity != identityKey(projectInfo) {
			return nil, errDialectArtifactOwner
		}
		location, e := m.locationRow(ctx, owner)
		if e != nil {
			return nil, e
		}
		v, _ := readDialectArtifactLocationRecord(location.raw)
		beforeLocation, _ := json.Marshal(v.Location)
		expectedLocation, _ := json.Marshal(dialectArtifactMoveLocation(previous, s.configDir, v.WorkDomain))
		if !bytes.Equal(beforeLocation, expectedLocation) || v.Location.Kind == "home" {
			return nil, errDialectArtifactOwner
		}
		p.Kind, p.WorkDomain, p.Before = "file", v.WorkDomain, bytes.Clone(location.raw)
		v.Location = dialectArtifactMoveLocation(destination, s.configDir, v.WorkDomain)
		p.After, err = marshalDialectArtifactLocation(v)
		if err != nil {
			return nil, err
		}
	} else {
		v, e := readDialectArtifactInjectedOwnerRecord(owner.raw)
		if e != nil || v.ProjectFolderIdentity != identityKey(projectInfo) {
			return nil, errors.Join(errDialectArtifactOwner, e)
		}
		p.Kind, p.WorkDomain, p.Provider = "injected", v.WorkDomain, v.Provider
		registry := s.deskOwner().retainedArtifactProviders
		if registry == nil {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		r := registry.byID[v.Provider]
		if r == nil || r.registry != registry || r.provider == nil || r.provider.id != v.Provider {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		if err = r.current(); err != nil {
			return nil, err
		}
		bindingRaw, e := json.Marshal(retainedArtifactInjectedBinding{Version: 1, Provider: v.Provider, Owner: v.Owner, ProjectFolderIdentity: v.ProjectFolderIdentity, WorkDomain: v.WorkDomain, Namespace: namespace})
		if e != nil {
			return nil, e
		}
		if err = readInjectedArtifactRow(ctx, r.root, owner.raw, v.Owner[2:]+".json", "d5-owners", v.Owner[:2]); err != nil {
			return nil, err
		}
		if err = readInjectedArtifactRow(ctx, r.root, bindingRaw, v.Owner+".json", "d5-native", namespace); err != nil {
			return nil, err
		}
		p.ProviderIdentitySHA256, p.ProviderOwnerSHA256, p.ProviderBindingSHA256 = digestOf(r.identity), digestOf(owner.raw), digestOf(bindingRaw)
	}
	work, err := m.workRow(ctx, p.WorkDomain)
	if err != nil {
		return nil, err
	}
	v, _ := readDialectArtifactWorkDomainRecord(work.raw)
	folder, e1 := s.assistant.root.Stat(".")
	lock, e2 := s.assistant.root.Lstat(".data-work.lock")
	if e1 != nil || e2 != nil || !bytes.Equal(v.PathBytes, []byte(s.configDir)) || v.FolderIdentity != identityKey(folder) || v.LockIdentity != identityKey(lock) ||
		!sameDirectory(s.configDir, folder) || ownerOnlyFile("move work lock", lock.Mode()) != nil || ownedByUs("move work lock", lock) != nil {
		return nil, errors.Join(errDialectArtifactOwner, e1, e2)
	}
	p.WorkSHA256 = digestOf(work.raw)
	if !p.valid() || !p.binds(previous, destination, identityKey(projectInfo), s.configDir) {
		return nil, errDialectArtifactOwner
	}
	return p, nil
}

func (s *Server) dialectArtifactMoveWithoutMarker(ctx context.Context, previous string, data *chatDataStore) error {
	if err := checkPrivateDataPath(data); err != nil {
		return err
	}
	namespace, err := assistantRunNamespace(data.root, previous)
	if err != nil {
		return err
	}
	// The caller holds move and private-data custody for the previous path.
	// Its namespace is not yet linked to the destination, so the ordinary
	// current-project store scope check cannot apply until history is linked.
	// Keep the same bounded original-byte read and full ledger validation.
	name := "assistant-runs-" + namespace + ".json"
	raw, err := readPrivateData(data.root, name, assistantRunRecordLimit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	captured := assistantRunCapturedRead{name: name, project: namespace, raw: raw, missing: errors.Is(err, os.ErrNotExist)}
	ledger, err := captured.decode(ctx)
	if err != nil {
		return err
	}
	for _, run := range ledger.Runs {
		if len(run.DialectDrafts) != 0 || run.DialectArtifactOwner != "" {
			return errDialectArtifactOwner
		}
	}
	return nil
}

// A lifecycle pin is selected before project/work/writes/private custody. This
// reads only the actual marker and immutable direct owner row, never an object.
func (s *Server) pinDialectArtifactProjectMoveProviders(ctx context.Context) (out *dialectArtifactSelectedProviderPins, err error) {
	var ids []string
	private, err := openDialectArtifactOwnerFolder(ctx, s.root, false, "move-provider-private", ".desk-private")
	if !injectedArtifactOnlyNotExist(err) {
		if err != nil {
			return nil, err
		}
		defer func() {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "move-provider-private", private.Close))
			if err != nil && out != nil {
				err = errors.Join(err, out.Close())
				out = nil
			}
		}()
		raw, e := readDialectArtifactOwnerFile(private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
		if !injectedArtifactOnlyNotExist(e) {
			if e != nil {
				return nil, e
			}
			marker, e := readDialectArtifactOwnerMarker(raw)
			if e != nil {
				return nil, e
			}
			m, e := s.openDialectArtifactSelectedMetadata(ctx)
			if e != nil {
				return nil, e
			}
			defer func() {
				err = errors.Join(err, m.Close())
				if err != nil && out != nil {
					err = errors.Join(err, out.Close())
					out = nil
				}
			}()
			row, e := m.ownerRow(ctx, marker.Owner)
			if e != nil {
				return nil, e
			}
			if injected, e := readDialectArtifactInjectedOwnerRecord(row.raw); e == nil {
				ids = append(ids, injected.Provider)
			}
		}
	}
	return pinDialectArtifactSelectedProviders(ctx, s.deskOwner(), ids)
}

type dialectArtifactProjectMoveHold struct {
	self                                *dialectArtifactProjectMoveHold
	s                                   *Server
	ctx                                 context.Context
	plan                                *dialectArtifactProjectMovePlan
	metadata                            *dialectArtifactSelectedMetadata
	immutable                           []*dialectArtifactSelectedMetadataRow
	location                            *dialectArtifactSelectedMetadataRow
	private, locks, rows, artifacts     *os.Root
	markerLock, ownerLock, queue        *os.File
	work                                *os.File // borrowed original work lease
	projectInfo, workInfo, workLockInfo os.FileInfo
	markerInfo                          os.FileInfo
	markerRaw                           []byte
	provider                            *dialectArtifactSelectedProviderPin
	edges                               []retainedArtifactInjectedEdge
	queueRoot                           *os.Root
	queueInfo                           os.FileInfo
	actualPath                          string
	closed                              bool
	closeErr                            error
}

// All host custody is lexically owned by the caller. This constructor acquires
// only real NB marker/owner/artifact exclusion and original descriptor pins.
// It cannot be invoked with a saved preheld flag or a fabricated session.
func (s *Server) holdDialectArtifactProjectMove(ctx context.Context, p *dialectArtifactProjectMovePlan, work *os.File, pins *dialectArtifactSelectedProviderPins, actualPath string) (out *dialectArtifactProjectMoveHold, err error) {
	if p == nil {
		return nil, nil
	}
	if !p.valid() || work == nil || pins == nil || pins.check(ctx) != nil {
		return nil, errDialectArtifactOwner
	}
	h := &dialectArtifactProjectMoveHold{s: s, ctx: ctx, plan: p, work: work, actualPath: actualPath}
	h.self = h
	defer func() {
		if err != nil {
			err = errors.Join(err, h.Close())
		}
	}()
	h.projectInfo, err = s.root.Stat(".")
	if err != nil {
		return nil, err
	}
	h.workInfo, err = s.assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	h.workLockInfo, err = work.Stat()
	if err != nil {
		return nil, err
	}
	h.private, err = openDialectArtifactOwnerFolder(ctx, s.root, false, "move-project-private", ".desk-private")
	if err != nil {
		return nil, err
	}
	h.edges, err = pinInjectedArtifactHeldEdges(ctx, s.root, h.private, ".desk-private")
	if err != nil {
		return nil, err
	}
	h.markerLock, err = lockDialectArtifactExistingMove(h.private, ".dialect-artifact-owner.lock")
	if err != nil {
		return nil, err
	}
	h.markerRaw, err = readDialectArtifactOwnerFile(h.private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	if err != nil || digestOf(h.markerRaw) != p.MarkerSHA256 {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.markerInfo, err = h.private.Lstat(dialectArtifactOwnerMarker)
	if err != nil {
		return nil, err
	}
	marker, err := readDialectArtifactOwnerMarker(h.markerRaw)
	if err != nil || marker.Owner != p.Owner {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.metadata, err = s.openDialectArtifactSelectedMetadata(ctx)
	if err != nil {
		return nil, err
	}
	h.locks, err = openDialectArtifactOwnerFolder(ctx, h.metadata.root, false, "move-locks", "locks", p.Owner[:2])
	if err != nil {
		return nil, err
	}
	lockEdges, err := pinInjectedArtifactHeldEdges(ctx, h.metadata.root, h.locks, "locks", p.Owner[:2])
	if err != nil {
		return nil, err
	}
	h.edges = append(h.edges, lockEdges...)
	h.ownerLock, err = lockDialectArtifactExistingMove(h.locks, p.Owner[2:]+".lock")
	if err != nil {
		return nil, err
	}
	owner, err := h.metadata.ownerRow(ctx, p.Owner)
	if err != nil || digestOf(owner.raw) != p.LegacySHA256 {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.immutable = append(h.immutable, owner)
	scope, err := h.metadata.scopeRow(ctx, dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: p.Owner, Namespace: p.Namespace})
	if err != nil || digestOf(scope.raw) != p.NativeScopeSHA256 {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.immutable = append(h.immutable, scope)
	workRow, err := h.metadata.workRow(ctx, p.WorkDomain)
	if err != nil || digestOf(workRow.raw) != p.WorkSHA256 {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.immutable = append(h.immutable, workRow)
	workRecord, _ := readDialectArtifactWorkDomainRecord(workRow.raw)
	if !bytes.Equal(workRecord.PathBytes, []byte(s.configDir)) || workRecord.FolderIdentity != identityKey(h.workInfo) || workRecord.LockIdentity != identityKey(h.workLockInfo) {
		return nil, errDialectArtifactOwner
	}
	if p.Kind == "file" {
		native, e := readDialectArtifactOwnerRecord(owner.raw)
		if e != nil || native.FolderIdentity != identityKey(h.projectInfo) || native.Work.ID != p.WorkDomain || native.Work.FolderIdentity != identityKey(h.workInfo) {
			return nil, errors.Join(errDialectArtifactOwner, e)
		}
		h.location, err = h.metadata.locationRow(ctx, owner)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(h.location.raw, p.Before) && !bytes.Equal(h.location.raw, p.After) {
			return nil, errDialectArtifactOwner
		}
		h.rows, err = openDialectArtifactOwnerFolder(ctx, h.metadata.root, false, "move-locations", "owner-locations", p.Owner[:2])
		if err != nil {
			return nil, err
		}
		rowEdges, e := pinInjectedArtifactHeldEdges(ctx, h.metadata.root, h.rows, "owner-locations", p.Owner[:2])
		if e != nil {
			return nil, e
		}
		h.edges = append(h.edges, rowEdges...)
		err = h.openRetainedForProjectMove(ctx, s)
		if err != nil {
			return nil, err
		}
	} else {
		v, e := readDialectArtifactInjectedOwnerRecord(owner.raw)
		if e != nil || v.ProjectFolderIdentity != identityKey(h.projectInfo) || v.WorkDomain != p.WorkDomain || v.Provider != p.Provider {
			return nil, errors.Join(errDialectArtifactOwner, e)
		}
		h.provider = pins.byID[p.Provider]
		if h.provider == nil || h.provider.check(ctx) != nil {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		h.queue, err = h.provider.registration.root.Open(".")
		if err != nil {
			return nil, err
		}
		h.queueRoot = h.provider.registration.root
	}
	if h.queue != nil {
		if err = lockPrivateData(h.queue, true); err != nil {
			return nil, errors.Join(errRetainedArtifactBusy, err)
		}
		h.queueInfo, err = h.queue.Stat()
		if err != nil {
			return nil, err
		}
	}
	if err = h.check(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *dialectArtifactProjectMoveHold) check() error {
	if h == nil || h.self != h || h.closed || h.work == nil || h.plan == nil || h.private == nil || h.locks == nil ||
		h.markerLock == nil || h.ownerLock == nil || h.projectInfo == nil || h.workInfo == nil || h.workLockInfo == nil || h.markerInfo == nil {
		return errDialectArtifactOwner
	}
	if err := h.ctx.Err(); err != nil {
		return err
	}
	if err := checkInjectedArtifactEdges(h.ctx, h.edges); err != nil {
		return err
	}
	if h.queue != nil {
		queue, e := h.queue.Stat()
		root, e2 := h.queueRoot.Stat(".")
		if e != nil || e2 != nil || h.queueInfo == nil || !os.SameFile(queue, h.queueInfo) || !os.SameFile(root, h.queueInfo) {
			return errors.Join(errDialectArtifactOwner, e, e2)
		}
	}
	if !sameDirectory(h.actualPath, h.projectInfo) || !sameDirectory(h.s.configDir, h.workInfo) {
		return errDialectArtifactOwner
	}
	project, e1 := h.s.root.Stat(".")
	work, e2 := h.s.assistant.root.Stat(".")
	lock, e3 := h.work.Stat()
	entry, e4 := h.s.assistant.root.Lstat(".data-work.lock")
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !os.SameFile(project, h.projectInfo) || !os.SameFile(work, h.workInfo) || !os.SameFile(lock, h.workLockInfo) || !os.SameFile(entry, h.workLockInfo) {
		return errors.Join(errDialectArtifactOwner, e1, e2, e3, e4)
	}
	for _, pin := range []struct {
		root *os.Root
		file *os.File
		name string
	}{{h.private, h.markerLock, ".dialect-artifact-owner.lock"}, {h.locks, h.ownerLock, h.plan.Owner[2:] + ".lock"}} {
		held, e := pin.file.Stat()
		current, e2 := pin.root.Lstat(pin.name)
		if e != nil || e2 != nil || !os.SameFile(held, current) {
			return errors.Join(errDialectArtifactOwner, e, e2)
		}
	}
	marker, e := readDialectArtifactOwnerFile(h.private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	markerInfo, e2 := h.private.Lstat(dialectArtifactOwnerMarker)
	if e != nil || e2 != nil || !bytes.Equal(marker, h.markerRaw) || !os.SameFile(markerInfo, h.markerInfo) {
		return errors.Join(errDialectArtifactOwner, e, e2)
	}
	for _, row := range h.immutable {
		if err := row.check(h.ctx); err != nil {
			return err
		}
	}
	if h.location != nil {
		if err := h.location.check(h.ctx); err != nil {
			return err
		}
	}
	if h.provider != nil {
		if err := h.provider.check(h.ctx); err != nil {
			return err
		}
		r := h.provider.registration
		if digestOf(r.identity) != h.plan.ProviderIdentitySHA256 {
			return errDialectArtifactOwner
		}
		owner := h.immutable[0].raw
		binding, _ := json.Marshal(retainedArtifactInjectedBinding{Version: 1, Provider: h.plan.Provider, Owner: h.plan.Owner, ProjectFolderIdentity: identityKey(h.projectInfo), WorkDomain: h.plan.WorkDomain, Namespace: h.plan.Namespace})
		if digestOf(owner) != h.plan.ProviderOwnerSHA256 || digestOf(binding) != h.plan.ProviderBindingSHA256 {
			return errDialectArtifactOwner
		}
		if err := readInjectedArtifactRow(h.ctx, r.root, owner, h.plan.Owner[2:]+".json", "d5-owners", h.plan.Owner[:2]); err != nil {
			return err
		}
		if err := readInjectedArtifactRow(h.ctx, r.root, binding, h.plan.Owner+".json", "d5-native", h.plan.Namespace); err != nil {
			return err
		}
	}
	return nil
}

func (h *dialectArtifactProjectMoveHold) publish(destination string) (err error) {
	if h == nil {
		return nil
	}
	h.actualPath = destination
	if err = h.check(); err != nil {
		return err
	}
	if h.plan.Kind == "injected" {
		return nil
	}
	name := h.plan.Owner[2:] + ".json"
	if bytes.Equal(h.location.raw, h.plan.After) {
		return syncExistingDialectArtifactOwner(h.ctx, h.rows, name, h.plan.After, "move-location")
	}
	file, stage, err := newDataStage(h.rows)
	if err != nil {
		return err
	}
	defer h.rows.Remove(stage)
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "move-location-stage", file.Close))
		}
	}()
	if _, err = file.Write(h.plan.After); err != nil {
		return err
	}
	if err = file.Chmod(custodyFileMode); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	written, err := file.Stat()
	if err != nil {
		return err
	}
	closed = true
	if err = closeDialectArtifactOwner(h.ctx, "move-location-stage", file.Close); err != nil {
		return err
	}
	if err = h.check(); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(h.ctx, "move-location.before-cas"); err != nil {
		return err
	}
	if err = h.check(); err != nil {
		return err
	}
	if err = h.rows.Rename(stage, name); err != nil {
		return err
	}
	if err = syncDialectArtifactOwnerDirectory(h.rows); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(h.ctx, "move-location.after-cas"); err != nil {
		return err
	}
	raw, err := readDialectArtifactOwnerFile(h.rows, name, len(h.plan.After))
	if err != nil || !bytes.Equal(raw, h.plan.After) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	current, err := h.rows.Lstat(name)
	if err != nil || !os.SameFile(current, written) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	h.location.raw, h.location.info = bytes.Clone(raw), current
	return h.check()
}

func (h *dialectArtifactProjectMoveHold) Close() error {
	if h == nil {
		return nil
	}
	if h.self != h {
		return errDialectArtifactOwner
	}
	if h.closed {
		return h.closeErr
	}
	// The caller joins this failure before acknowledging any move completion.
	h.closeErr = h.check()
	h.closed = true
	for _, item := range []struct {
		label string
		close func() error
	}{
		{"move-queue", func() error {
			if h.queue != nil {
				return h.queue.Close()
			}
			return nil
		}},
		{"move-artifacts", func() error {
			if h.artifacts != nil {
				return h.artifacts.Close()
			}
			return nil
		}},
		{"move-locations", func() error {
			if h.rows != nil {
				return h.rows.Close()
			}
			return nil
		}},
		{"move-owner-lock", func() error {
			if h.ownerLock != nil {
				return h.ownerLock.Close()
			}
			return nil
		}},
		{"move-locks", func() error {
			if h.locks != nil {
				return h.locks.Close()
			}
			return nil
		}},
		{"move-metadata", func() error {
			if h.metadata != nil {
				return h.metadata.Close()
			}
			return nil
		}},
		{"move-marker-lock", func() error {
			if h.markerLock != nil {
				return h.markerLock.Close()
			}
			return nil
		}},
		{"move-project-private", func() error {
			if h.private != nil {
				return h.private.Close()
			}
			return nil
		}},
	} {
		h.closeErr = errors.Join(h.closeErr, closeDialectArtifactOwner(h.ctx, item.label, item.close))
	}
	return h.closeErr
}

func lockDialectArtifactExistingMove(root *os.Root, name string) (_ *os.File, err error) {
	entry, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()
	held, err := file.Stat()
	if err != nil || !os.SameFile(entry, held) || name != ".data-work.lock" && held.Size() != 0 || ownerOnlyFile(name, held.Mode()) != nil || ownedByUs(name, held) != nil {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	if err = lockPrivateData(file, true); err != nil {
		return nil, err
	}
	return file, nil
}
