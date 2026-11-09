package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// These are original provider operations, not saved owner capabilities. The
// selected-source producer must acquire them before any host custody and retain
// them until every borrowed view, auth pin and source/work descriptor is closed.
// Pinning alone cannot read an object, create a view or authorize an owner.
type dialectArtifactSelectedProviderPins struct {
	self     *dialectArtifactSelectedProviderPins
	owner    *Server
	registry *retainedArtifactProviderRegistry
	ordered  []*dialectArtifactSelectedProviderPin
	byID     map[string]*dialectArtifactSelectedProviderPin
	closed   bool
	closeErr error
}

type dialectArtifactSelectedProviderPin struct {
	set          *dialectArtifactSelectedProviderPins
	registration *retainedArtifactProviderRegistration
	operation    *retainedArtifactInjectedOperation
}

// providerIDs must come from the selected, validated local owner rows. This
// leaf only selects already registered constructor-sealed instances. It never
// consults s.retainedArtifacts, installs a provider, opens a saved path, acquires
// host custody or mints a source/view. The future whole-store producer owns the
// separate exact saved-input, metadata, work and home admission.
func pinDialectArtifactSelectedProviders(ctx context.Context, owner *Server, providerIDs []string) (_ *dialectArtifactSelectedProviderPins, err error) {
	if ctx == nil || owner == nil || owner.deskOwner() != owner || ctx.Err() != nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	ids := append([]string(nil), providerIDs...)
	sort.Strings(ids)
	p := &dialectArtifactSelectedProviderPins{owner: owner, registry: owner.retainedArtifactProviders,
		byID: make(map[string]*dialectArtifactSelectedProviderPin)}
	p.self = p
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Close())
		}
	}()
	for _, id := range ids {
		if !assistantRunDigest(id) || p.registry == nil {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		if p.byID[id] != nil {
			continue
		}
		r := p.registry.byID[id]
		if r == nil || r.registry != p.registry || r.provider == nil || r.provider.id != id {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		if err = r.current(); err != nil {
			return nil, err
		}
		op, pinErr := r.provider.pin()
		if pinErr != nil {
			return nil, pinErr
		}
		selected := &dialectArtifactSelectedProviderPin{set: p, registration: r, operation: op}
		p.ordered = append(p.ordered, selected)
		p.byID[id] = selected
		if err = selected.check(ctx); err != nil {
			return nil, err
		}
	}
	if err = p.check(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *dialectArtifactSelectedProviderPins) check(ctx context.Context) error {
	if p == nil || p.self != p || p.closed || ctx == nil || p.owner == nil ||
		p.owner.deskOwner() != p.owner || p.owner.retainedArtifactProviders != p.registry {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, selected := range p.ordered {
		if err := selected.check(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *dialectArtifactSelectedProviderPin) check(ctx context.Context) error {
	if p == nil || p.set == nil || p.set.self != p.set || p.set.closed || ctx == nil ||
		p.registration == nil || p.registration.provider == nil || p.operation == nil ||
		p.operation.finished.Load() || p.operation.provider != p.registration.provider ||
		p.set.owner == nil || p.set.owner.retainedArtifactProviders != p.set.registry ||
		p.registration.registry != p.set.registry ||
		p.set.byID[p.registration.provider.id] != p {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := p.registration.current(); err != nil {
		return err
	}
	return p.registration.provider.check(ctx)
}

// Called after all borrowed source/auth/view handles have closed. finish is
// idempotent, but only this original set can end its operations. A copied set
// must not finish another caller's live operation. Provider Close revokes first
// and waits on these original pins without entering host custody.
func (p *dialectArtifactSelectedProviderPins) Close() error {
	if p == nil {
		return nil
	}
	if p.self != p {
		return errRetainedArtifactAuthorityUnavailable
	}
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	for i := len(p.ordered) - 1; i >= 0; i-- {
		p.closeErr = errors.Join(p.closeErr, p.ordered[i].operation.finish())
	}
	return p.closeErr
}

// Opened settings/lock descriptors belong to this exact lexical collection.
// Construction does not create roots or lock files and does not read objects.
// Callers open every selected domain first, then acquire this collection before
// taking current writes/private; they must never take the current work lock a
// second time through privateDataFileLock.
type dialectArtifactSelectedWorkDomains struct {
	self      *dialectArtifactSelectedWorkDomains
	ordered   []*dialectArtifactSelectedWorkDomain
	byID      map[string]*dialectArtifactSelectedWorkDomain
	attempted bool
	locked    bool
	closed    bool
	closeErr  error
}

type dialectArtifactSelectedWorkDomain struct {
	set          *dialectArtifactSelectedWorkDomains
	record       dialectArtifactWorkDomainRecord
	root         *os.Root
	file         *os.File
	folderInfo   os.FileInfo
	lockInfo     os.FileInfo
	lockOwner    *dialectArtifactSelectedWorkDomain
	borrowedFile bool
	held         bool
}

// Match native settings custody without importing the chat-data path grammar:
// an already accepted Unix settings path can contain newlines or other exact
// non-NUL bytes. No narrowing, repair, symlink substitution or O_CREATE occurs.
func openDialectArtifactSelectedSettings(record dialectArtifactWorkDomainRecord) (_ *os.Root, err error) {
	if !record.valid() || !custodyChecked {
		return nil, errDialectArtifactOwner
	}
	path := string(record.PathBytes)
	if err = checkAncestors(filepath.Dir(path)); err != nil {
		return nil, err
	}
	entry, err := os.Lstat(path)
	if err != nil || safeDirectory(path, entry, true) != nil || needsNarrowing(entry.Mode()) || identityKey(entry) != record.FolderIdentity {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, root.Close())
		}
	}()
	held, statErr := root.Stat(".")
	again, lookErr := os.Lstat(path)
	if statErr != nil || lookErr != nil || !os.SameFile(entry, held) || !os.SameFile(held, again) ||
		safeDirectory(path, held, true) != nil || safeDirectory(path, again, true) != nil || needsNarrowing(held.Mode()) || needsNarrowing(again.Mode()) {
		return nil, errors.Join(errDialectArtifactOwner, statErr, lookErr)
	}
	return root, nil
}

func openDialectArtifactSelectedWorkDomains(ctx context.Context, records []dialectArtifactWorkDomainRecord) (_ *dialectArtifactSelectedWorkDomains, err error) {
	if ctx == nil {
		return nil, errDialectArtifactOwner
	}
	selected := append([]dialectArtifactWorkDomainRecord(nil), records...)
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	w := &dialectArtifactSelectedWorkDomains{byID: make(map[string]*dialectArtifactSelectedWorkDomain)}
	w.self = w
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Close())
		}
	}()
	for _, record := range selected {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if !record.valid() {
			return nil, errDialectArtifactOwner
		}
		if prior := w.byID[record.ID]; prior != nil {
			if prior.record.Version != record.Version || prior.record.Kind != record.Kind ||
				prior.record.FolderIdentity != record.FolderIdentity || prior.record.LockIdentity != record.LockIdentity ||
				!bytes.Equal(prior.record.PathBytes, record.PathBytes) {
				return nil, errDialectArtifactOwner
			}
			continue
		}
		root, openErr := openDialectArtifactSelectedSettings(record)
		if openErr != nil {
			return nil, openErr
		}
		// Register owned handles before the first fallible step so every unwind
		// closes all descriptors even if this domain has not acquired its flock.
		domain := &dialectArtifactSelectedWorkDomain{set: w, record: record, root: root}
		domain.record.PathBytes = bytes.Clone(record.PathBytes)
		w.ordered = append(w.ordered, domain)
		w.byID[record.ID] = domain
		domain.folderInfo, err = root.Stat(".")
		if err != nil {
			return nil, err
		}
		entry, lookErr := root.Lstat(".data-work.lock")
		if lookErr != nil || ownerOnlyFile("selected work lock", entry.Mode()) != nil || ownedByUs("selected work lock", entry) != nil ||
			identityKey(entry) != record.LockIdentity {
			return nil, errors.Join(errDialectArtifactOwner, lookErr)
		}
		domain.file, err = root.OpenFile(".data-work.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
		if err != nil {
			return nil, err
		}
		domain.lockInfo, err = domain.file.Stat()
		if err != nil || !os.SameFile(entry, domain.lockInfo) {
			return nil, errors.Join(errDialectArtifactOwner, err)
		}
		if err = domain.check(ctx, false); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// The original outer host may establish its own current lock before source
// discovery. This borrows that exact FD before any flock, derives its identity
// from the actual current assistant root, and never creates a row or a lock.
// The caller retains the FD until this collection closes; this collection must
// not close it. All EX acquisition still occurs once in sorted domain order.
func (w *dialectArtifactSelectedWorkDomains) includeCurrent(ctx context.Context, s *Server, file *os.File) (err error) {
	if w == nil || w.self != w || w.closed || w.attempted || ctx == nil || s == nil ||
		s.assistant == nil || !s.assistant.usable() || file == nil {
		return errDialectArtifactOwner
	}
	folder, statErr := s.assistant.root.Stat(".")
	lock, lockErr := file.Stat()
	entry, lookErr := s.assistant.root.Lstat(".data-work.lock")
	if statErr != nil || lockErr != nil || lookErr != nil || !os.SameFile(lock, entry) ||
		ownerOnlyFile("current selected work lock", lock.Mode()) != nil || ownedByUs("current selected work lock", lock) != nil ||
		ownerOnlyFile("current selected work lock", entry.Mode()) != nil || ownedByUs("current selected work lock", entry) != nil ||
		!sameDirectory(s.configDir, folder) {
		return errors.Join(errDialectArtifactOwner, statErr, lockErr, lookErr)
	}
	record := dialectArtifactWorkDomainRecord{Version: 1, Kind: "settings", PathBytes: []byte(s.configDir),
		FolderIdentity: identityKey(folder), LockIdentity: identityKey(lock)}
	record.ID = dialectArtifactWorkDomainID(record.FolderIdentity, record.LockIdentity)
	if !record.valid() {
		return errDialectArtifactOwner
	}
	if domain := w.byID[record.ID]; domain != nil {
		if !os.SameFile(domain.folderInfo, folder) || !os.SameFile(domain.lockInfo, lock) || !bytes.Equal(domain.record.PathBytes, record.PathBytes) || domain.borrowedFile {
			return errDialectArtifactOwner
		}
		if err = closeDialectArtifactOwner(ctx, "selected-work-redundant-lock", domain.file.Close); err != nil {
			return err
		}
		domain.file, domain.borrowedFile = file, true
		return domain.check(ctx, false)
	}
	root, err := s.assistant.root.OpenRoot(".")
	if err != nil {
		return err
	}
	domain := &dialectArtifactSelectedWorkDomain{set: w, record: record, root: root, file: file,
		folderInfo: folder, lockInfo: lock, borrowedFile: true}
	w.ordered = append(w.ordered, domain)
	w.byID[record.ID] = domain
	sort.Slice(w.ordered, func(i, j int) bool { return w.ordered[i].record.ID < w.ordered[j].record.ID })
	return domain.check(ctx, false)
}

func (w *dialectArtifactSelectedWorkDomains) acquire(ctx context.Context) error {
	if w == nil || w.self != w || w.closed || w.attempted || ctx == nil {
		return errDialectArtifactOwner
	}
	w.attempted = true
	for i, domain := range w.ordered {
		for _, prior := range w.ordered[:i] {
			if os.SameFile(prior.lockInfo, domain.lockInfo) {
				domain.lockOwner = prior
				if prior.lockOwner != nil {
					domain.lockOwner = prior.lockOwner
				}
				break
			}
		}
		if err := domain.check(ctx, false); err != nil {
			return err
		}
		if domain.lockOwner == nil {
			if err := lockPrivateData(domain.file, true); err != nil {
				if lockHeld(err) {
					return errors.Join(errRetainedArtifactBusy, err)
				}
				return err
			}
			domain.held = true
		}
		if err := domain.check(ctx, true); err != nil {
			return err
		}
	}
	w.locked = true
	return w.check(ctx)
}

func (w *dialectArtifactSelectedWorkDomains) check(ctx context.Context) error {
	if w == nil || w.self != w || w.closed || !w.locked || ctx == nil {
		return errDialectArtifactOwner
	}
	for _, domain := range w.ordered {
		if err := domain.check(ctx, true); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (d *dialectArtifactSelectedWorkDomain) check(ctx context.Context, requireHeld bool) error {
	if d == nil || d.set == nil || d.set.self != d.set || d.set.closed || ctx == nil ||
		d.root == nil || d.file == nil || d.set.byID[d.record.ID] != d {
		return errDialectArtifactOwner
	}
	if requireHeld {
		original := d
		if d.lockOwner != nil {
			original = d.lockOwner
			if original.set != d.set || !os.SameFile(original.lockInfo, d.lockInfo) || d.set.byID[original.record.ID] != original {
				return errDialectArtifactOwner
			}
		}
		if !original.held {
			return errDialectArtifactOwner
		}
		if original != d {
			if err := original.check(ctx, true); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := string(d.record.PathBytes)
	folder, statErr := d.root.Stat(".")
	current, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !os.SameFile(folder, d.folderInfo) || !os.SameFile(current, d.folderInfo) ||
		safeDirectory(path, folder, true) != nil || safeDirectory(path, current, true) != nil ||
		needsNarrowing(folder.Mode()) || needsNarrowing(current.Mode()) || identityKey(folder) != d.record.FolderIdentity {
		return errors.Join(errDialectArtifactOwner, statErr, pathErr)
	}
	held, heldErr := d.file.Stat()
	entry, lookErr := d.root.Lstat(".data-work.lock")
	if heldErr != nil || lookErr != nil || !os.SameFile(held, d.lockInfo) || !os.SameFile(entry, d.lockInfo) ||
		ownerOnlyFile("selected work lock", held.Mode()) != nil || ownerOnlyFile("selected work lock", entry.Mode()) != nil ||
		ownedByUs("selected work lock", held) != nil || ownedByUs("selected work lock", entry) != nil || identityKey(held) != d.record.LockIdentity ||
		d.record.ID != dialectArtifactWorkDomainID(identityKey(folder), identityKey(held)) {
		return errors.Join(errDialectArtifactOwner, heldErr, lookErr)
	}
	return nil
}

func (w *dialectArtifactSelectedWorkDomains) Close() error {
	if w == nil {
		return nil
	}
	if w.self != w {
		return errDialectArtifactOwner
	}
	if w.closed {
		return w.closeErr
	}
	w.closed = true
	for i := len(w.ordered) - 1; i >= 0; i-- {
		domain := w.ordered[i]
		if domain.file != nil && !domain.borrowedFile {
			w.closeErr = errors.Join(w.closeErr, closeDialectArtifactOwner(context.Background(), "selected-work-lock", domain.file.Close))
		}
		if domain.root != nil {
			w.closeErr = errors.Join(w.closeErr, closeDialectArtifactOwner(context.Background(), "selected-work-root", domain.root.Close))
		}
		domain.held = false
	}
	return w.closeErr
}

// Installation metadata is rooted only in this Server's actual desk owner.
// Rows remain snapshots, never grants: the later authenticated history/source
// producer must join them to its exact saved run before any artifact access.
type dialectArtifactSelectedMetadata struct {
	self      *dialectArtifactSelectedMetadata
	owner     *Server
	assistant *assistantStore
	secrets   *os.Root
	root      *os.Root
	edges     []retainedArtifactInjectedEdge
	rows      []*dialectArtifactSelectedMetadataRow
	closed    bool
	closeErr  error
}

type dialectArtifactSelectedMetadataRow struct {
	metadata *dialectArtifactSelectedMetadata
	parts    []string
	name     string
	raw      []byte
	info     os.FileInfo
	edges    []retainedArtifactInjectedEdge
}

func (s *Server) openDialectArtifactSelectedMetadata(ctx context.Context) (_ *dialectArtifactSelectedMetadata, err error) {
	if ctx == nil || s == nil {
		return nil, errDialectArtifactOwner
	}
	owner := s.deskOwner()
	if owner == nil || owner.assistant == nil || !owner.assistant.usable() || owner.assistant.secrets == nil {
		return nil, errDialectArtifactOwner
	}
	m := &dialectArtifactSelectedMetadata{owner: owner, assistant: owner.assistant}
	m.self = m
	defer func() {
		if err != nil {
			err = errors.Join(err, m.Close())
		}
	}()
	m.secrets, err = owner.assistant.secrets.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	m.root, err = openDialectArtifactOwnerFolder(ctx, m.secrets, false, "selected-metadata", dialectArtifactOwnersFolder)
	if err != nil {
		return nil, err
	}
	m.edges, err = pinInjectedArtifactHeldEdges(ctx, m.secrets, m.root, dialectArtifactOwnersFolder)
	if err != nil {
		return nil, err
	}
	if err = m.check(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *dialectArtifactSelectedMetadata) check(ctx context.Context) error {
	if m == nil || m.self != m || m.closed || ctx == nil || m.owner == nil ||
		m.owner.assistant != m.assistant || m.assistant == nil || !m.assistant.usable() ||
		m.secrets == nil || m.root == nil {
		return errDialectArtifactOwner
	}
	current, err := m.assistant.secrets.Stat(".")
	held, statErr := m.secrets.Stat(".")
	if err != nil || statErr != nil || !os.SameFile(current, held) {
		return errors.Join(errDialectArtifactOwner, err, statErr)
	}
	return checkInjectedArtifactEdges(ctx, m.edges)
}

// All components are generated from fully checked local IDs by the typed
// selectors below. No aggregate table read, raw locator or directory scan.
func (m *dialectArtifactSelectedMetadata) read(ctx context.Context, limit int, name string, parts ...string) (_ *dialectArtifactSelectedMetadataRow, err error) {
	if err = m.check(ctx); err != nil {
		return nil, err
	}
	folder, err := openDialectArtifactOwnerFolder(ctx, m.root, false, "selected-metadata-row", parts...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-metadata-row", folder.Close)) }()
	row := &dialectArtifactSelectedMetadataRow{metadata: m, name: name, parts: append([]string(nil), parts...)}
	row.edges, err = pinInjectedArtifactHeldEdges(ctx, m.root, folder, parts...)
	if err != nil {
		return nil, err
	}
	file, err := openDialectArtifactOwnerFile(folder, name)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-metadata-file", file.Close)) }()
	row.info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	row.raw, err = readBounded(file, limit)
	if err != nil {
		return nil, err
	}
	again, err := folder.Lstat(name)
	if err != nil || !os.SameFile(row.info, again) {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	m.rows = append(m.rows, row)
	return row, nil
}

func (row *dialectArtifactSelectedMetadataRow) check(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(errDialectArtifactOwner, err)
		}
	}()
	if row == nil || row.metadata == nil || row.info == nil || len(row.raw) == 0 {
		return errDialectArtifactOwner
	}
	if err = row.metadata.check(ctx); err != nil {
		return err
	}
	if err = checkInjectedArtifactEdges(ctx, row.edges); err != nil {
		return err
	}
	folder, err := openDialectArtifactOwnerFolder(ctx, row.metadata.root, false, "selected-metadata-reread", row.parts...)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-metadata-reread", folder.Close))
	}()
	file, err := openDialectArtifactOwnerFile(folder, row.name)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-metadata-reread-file", file.Close))
	}()
	info, err := file.Stat()
	if err != nil || !os.SameFile(info, row.info) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	raw, err := readBounded(file, len(row.raw))
	if err != nil || !bytes.Equal(raw, row.raw) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	again, err := folder.Lstat(row.name)
	if err != nil || !os.SameFile(again, row.info) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return nil
}

func (m *dialectArtifactSelectedMetadata) ownerRow(ctx context.Context, owner string) (*dialectArtifactSelectedMetadataRow, error) {
	if !assistantRunDigest(owner) {
		return nil, errDialectArtifactOwner
	}
	row, err := m.read(ctx, dialectArtifactOwnerRecordBytes, owner[2:]+".json", "owners", owner[:2])
	if err != nil {
		return nil, err
	}
	if native, nativeErr := readDialectArtifactOwnerRecord(row.raw); nativeErr == nil && native.Owner == owner {
		return row, nil
	}
	injected, injectedErr := readDialectArtifactInjectedOwnerRecord(row.raw)
	if injectedErr != nil || injected.Owner != owner {
		return nil, errors.Join(errDialectArtifactOwner, injectedErr)
	}
	return row, nil
}

func (m *dialectArtifactSelectedMetadata) scopeRow(ctx context.Context, scope dialectArtifactOwnerScope) (*dialectArtifactSelectedMetadataRow, error) {
	if !scope.valid() {
		return nil, errDialectArtifactOwner
	}
	parts := []string{"native", scope.Namespace}
	if scope.Kind == "import" {
		parts = []string{"imports", scope.Namespace, scope.RunID}
	}
	row, err := m.read(ctx, dialectArtifactOwnerScopeBytes, scope.Owner+".json", parts...)
	if err != nil {
		return nil, err
	}
	value, err := readDialectArtifactOwnerScope(row.raw)
	if err != nil || value != scope {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	return row, nil
}

func (m *dialectArtifactSelectedMetadata) locationRow(ctx context.Context, owner *dialectArtifactSelectedMetadataRow) (*dialectArtifactSelectedMetadataRow, error) {
	if owner == nil || owner.metadata != m {
		return nil, errDialectArtifactOwner
	}
	native, err := readDialectArtifactOwnerRecord(owner.raw)
	if err != nil {
		return nil, err
	}
	row, err := m.read(ctx, dialectArtifactLocationRecordBytes, native.Owner[2:]+".json", "owner-locations", native.Owner[:2])
	if err != nil {
		return nil, err
	}
	value, err := readDialectArtifactLocationRecord(row.raw)
	if err != nil || value.Owner != native.Owner || value.LegacySHA256 != digestOf(owner.raw) ||
		value.FolderIdentity != native.FolderIdentity || value.WorkDomain != native.Work.ID {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	return row, nil
}

func (m *dialectArtifactSelectedMetadata) workRow(ctx context.Context, id string) (*dialectArtifactSelectedMetadataRow, error) {
	if !assistantRunDigest(id) {
		return nil, errDialectArtifactOwner
	}
	row, err := m.read(ctx, dialectArtifactWorkDomainRecordBytes, id[2:]+".json", "work-domains", id[:2])
	if err != nil {
		return nil, err
	}
	value, err := readDialectArtifactWorkDomainRecord(row.raw)
	if err != nil || value.ID != id {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	return row, nil
}

func (m *dialectArtifactSelectedMetadata) homeRow(ctx context.Context, id string) (*dialectArtifactSelectedMetadataRow, error) {
	if !assistantRunDigest(id) {
		return nil, errDialectArtifactOwner
	}
	row, err := m.read(ctx, dialectArtifactHomeAnchorRecordBytes, id[2:]+".json", "home-anchors", id[:2])
	if err != nil {
		return nil, err
	}
	value, err := readDialectArtifactHomeAnchorRecord(row.raw)
	if err != nil || value.ID != id {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	return row, nil
}

func (m *dialectArtifactSelectedMetadata) Close() error {
	if m == nil {
		return nil
	}
	if m.self != m {
		return errDialectArtifactOwner
	}
	if m.closed {
		return m.closeErr
	}
	m.closed = true
	if m.root != nil {
		m.closeErr = errors.Join(m.closeErr, closeDialectArtifactOwner(context.Background(), "selected-metadata", m.root.Close))
	}
	if m.secrets != nil {
		m.closeErr = errors.Join(m.closeErr, closeDialectArtifactOwner(context.Background(), "selected-metadata-secrets", m.secrets.Close))
	}
	return m.closeErr
}

// Native physical custody is deliberately separate from historical read
// admission. These handles alone cannot create an object view. They are opened
// before EX-work acquisition, then joined to the exact authenticated history by
// the selected-source producer. Historical v1 path bytes are never opened.
type dialectArtifactSelectedNativeSource struct {
	self                  *dialectArtifactSelectedNativeSource
	ctx                   context.Context
	ownerRow, locationRow *dialectArtifactSelectedMetadataRow
	owner                 dialectArtifactOwnerRecord
	location              dialectArtifactLocationRecord
	work                  *dialectArtifactSelectedWorkDomain
	project               *ProjectRoot
	root                  *os.Root
	info                  os.FileInfo
	edges                 []retainedArtifactInjectedEdge
	home                  *dialectArtifactSelectedSourceHome
	markerRaw             []byte
	markerInfo            os.FileInfo
	privateEdges          []retainedArtifactInjectedEdge
	closed                bool
	closeErr              error
}

type dialectArtifactSelectedSourceHome struct {
	ctx          context.Context
	row          *dialectArtifactSelectedMetadataRow
	record       dialectArtifactHomeAnchorRecord
	work         *dialectArtifactSelectedWorkDomain
	root         *os.Root
	info         os.FileInfo
	lease        *os.File
	leaseInfo    os.FileInfo
	registryRaw  []byte
	registryInfo os.FileInfo
	registry     deskHomeRegistry
	entry        deskHomeEntry
}

// Project directories keep their ordinary permissions. Each contained edge is
// inspected/opened/reinspected relative to the original selected work/home FD;
// this is not the private metadata directory opener and needs no foreign Server.
func openDialectArtifactSelectedRelativeProject(ctx context.Context, start *os.Root, path string) (_ *os.Root, edges []retainedArtifactInjectedEdge, err error) {
	if start == nil || !dialectArtifactRelativePath([]byte(path)) {
		return nil, nil, errDialectArtifactOwner
	}
	current, err := start.OpenRoot(".")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-project-edge", current.Close))
		}
	}()
	if path == "." {
		return current, nil, nil
	}
	parts := strings.Split(path, string(filepath.Separator))
	for index, part := range parts {
		entry, lookErr := current.Lstat(part)
		if lookErr != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.Join(errDialectArtifactOwner, lookErr)
		}
		next, openErr := current.OpenRoot(part)
		if openErr != nil {
			return nil, nil, openErr
		}
		held, statErr := next.Stat(".")
		again, againErr := current.Lstat(part)
		if statErr != nil || againErr != nil || !os.SameFile(entry, held) || !os.SameFile(held, again) ||
			!again.IsDir() || again.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errors.Join(errDialectArtifactOwner, statErr, againErr, closeDialectArtifactOwner(ctx, "selected-project-edge", next.Close))
		}
		edges = append(edges, retainedArtifactInjectedEdge{root: start, parts: append([]string(nil), parts[:index+1]...), info: held})
		prior := current
		current = next
		if err = closeDialectArtifactOwner(ctx, "selected-project-edge", prior.Close); err != nil {
			return nil, nil, err
		}
	}
	return current, edges, nil
}

func checkDialectArtifactSelectedProjectEdges(ctx context.Context, edges []retainedArtifactInjectedEdge) error {
	for _, edge := range edges {
		root, _, err := openDialectArtifactSelectedRelativeProject(ctx, edge.root, filepath.Join(edge.parts...))
		if err != nil {
			return err
		}
		info, statErr := root.Stat(".")
		closeErr := closeDialectArtifactOwner(ctx, "selected-project-edge-check", root.Close)
		if statErr != nil || closeErr != nil || !os.SameFile(info, edge.info) {
			return errors.Join(errDialectArtifactOwner, statErr, closeErr)
		}
	}
	return nil
}

func openDialectArtifactSelectedSourceHome(ctx context.Context, row *dialectArtifactSelectedMetadataRow, work *dialectArtifactSelectedWorkDomain, folder string) (_ *dialectArtifactSelectedSourceHome, err error) {
	if row == nil || work == nil || !validHomeFolder(folder) {
		return nil, errDialectArtifactOwner
	}
	record, err := readDialectArtifactHomeAnchorRecord(row.raw)
	if err != nil {
		return nil, err
	}
	h := &dialectArtifactSelectedSourceHome{ctx: ctx, row: row, record: record, work: work}
	defer func() {
		if err != nil {
			err = errors.Join(err, h.close())
		}
	}()
	path := string(record.PathBytes)
	h.info, err = os.Lstat(path)
	if err != nil || !h.info.IsDir() || h.info.Mode()&os.ModeSymlink != 0 || identityKey(h.info) != record.FolderIdentity {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.root, err = os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	held, statErr := h.root.Stat(".")
	if statErr != nil || !os.SameFile(held, h.info) {
		return nil, errors.Join(errDialectArtifactOwner, statErr)
	}
	platform, _, err := openDialectArtifactSelectedRelativeProject(ctx, h.root, deskHomePlatform)
	if err != nil {
		return nil, err
	}
	platformInfo, statErr := platform.Stat(".")
	closeErr := closeDialectArtifactOwner(ctx, "selected-home-platform", platform.Close)
	if statErr != nil || closeErr != nil || !os.SameFile(platformInfo, work.folderInfo) ||
		!bytes.Equal(work.record.PathBytes, []byte(filepath.Join(path, deskHomePlatform))) {
		return nil, errors.Join(errDialectArtifactOwner, statErr, closeErr)
	}
	entry, err := work.root.Lstat(".home-use.lock")
	if err != nil || ownerOnlyFile("selected home lease", entry.Mode()) != nil || ownedByUs("selected home lease", entry) != nil {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.lease, err = work.root.OpenFile(".home-use.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	h.leaseInfo, err = h.lease.Stat()
	if err != nil || !os.SameFile(entry, h.leaseInfo) {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	if err = lockPrivateData(h.lease, false); err != nil {
		return nil, err
	}
	file, err := openDialectArtifactOwnerFile(work.root, deskHomeRegistryName)
	if err != nil {
		return nil, err
	}
	h.registryInfo, err = file.Stat()
	if err == nil {
		h.registryRaw, err = readBounded(file, 1<<20)
	}
	if err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-home-registry", file.Close)); err != nil {
		return nil, err
	}
	h.registry, err = decodeHomeRegistry(h.registryRaw)
	if err != nil ||
		record.ID != dialectArtifactHomeAnchorID(h.registry.Owner, record.FolderIdentity) {
		return nil, errDialectArtifactOwner
	}
	for _, entry := range h.registry.Desks {
		if entry.Folder == folder {
			h.entry = entry
			break
		}
	}
	if h.entry.Folder != folder {
		return nil, errDialectArtifactOwner
	}
	if err = h.check(ctx, false); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *dialectArtifactSelectedSourceHome) check(ctx context.Context, requireWork bool) (err error) {
	if h == nil || h.root == nil || h.lease == nil || h.work == nil || h.row == nil {
		return errDialectArtifactOwner
	}
	if err = h.row.check(ctx); err != nil {
		return err
	}
	if err = h.work.check(ctx, requireWork); err != nil {
		return err
	}
	current, pathErr := os.Lstat(string(h.record.PathBytes))
	held, statErr := h.root.Stat(".")
	if pathErr != nil || statErr != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(current, h.info) || !os.SameFile(held, h.info) ||
		h.record.ID != dialectArtifactHomeAnchorID(h.registry.Owner, identityKey(held)) {
		return errors.Join(errDialectArtifactOwner, pathErr, statErr)
	}
	platform, _, err := openDialectArtifactSelectedRelativeProject(ctx, h.root, deskHomePlatform)
	if err != nil {
		return err
	}
	info, statErr := platform.Stat(".")
	closeErr := closeDialectArtifactOwner(ctx, "selected-home-platform-check", platform.Close)
	if statErr != nil || closeErr != nil || !os.SameFile(info, h.work.folderInfo) {
		return errors.Join(errDialectArtifactOwner, statErr, closeErr)
	}
	lease, statErr := h.lease.Stat()
	entry, lookErr := h.work.root.Lstat(".home-use.lock")
	if statErr != nil || lookErr != nil || !os.SameFile(lease, h.leaseInfo) || !os.SameFile(entry, h.leaseInfo) ||
		ownerOnlyFile("selected home lease", entry.Mode()) != nil || ownedByUs("selected home lease", entry) != nil {
		return errors.Join(errDialectArtifactOwner, statErr, lookErr)
	}
	file, err := openDialectArtifactOwnerFile(h.work.root, deskHomeRegistryName)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-home-registry-check", file.Close))
	}()
	info, err = file.Stat()
	if err != nil || !os.SameFile(info, h.registryInfo) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	raw, err := readBounded(file, 1<<20)
	if err != nil || !bytes.Equal(raw, h.registryRaw) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	again, err := h.work.root.Lstat(deskHomeRegistryName)
	if err != nil || !os.SameFile(again, h.registryInfo) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return nil
}

func (h *dialectArtifactSelectedSourceHome) close() (err error) {
	if h == nil {
		return nil
	}
	if h.root != nil {
		err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "selected-home-root", h.root.Close))
		h.root = nil
	}
	if h.lease != nil {
		err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "selected-home-lease", h.lease.Close))
		h.lease = nil
	}
	return err
}

func openDialectArtifactSelectedNativeSource(ctx context.Context, ownerRow, locationRow *dialectArtifactSelectedMetadataRow, work *dialectArtifactSelectedWorkDomain) (_ *dialectArtifactSelectedNativeSource, err error) {
	if ctx == nil || ownerRow == nil || locationRow == nil || work == nil || ownerRow.metadata != locationRow.metadata {
		return nil, errDialectArtifactOwner
	}
	owner, err := readDialectArtifactOwnerRecord(ownerRow.raw)
	if err != nil {
		return nil, err
	}
	location, err := readDialectArtifactLocationRecord(locationRow.raw)
	if err != nil || location.Owner != owner.Owner || location.LegacySHA256 != digestOf(ownerRow.raw) ||
		location.FolderIdentity != owner.FolderIdentity || location.WorkDomain != owner.Work.ID || location.WorkDomain != work.record.ID {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	n := &dialectArtifactSelectedNativeSource{ctx: ctx, ownerRow: ownerRow, locationRow: locationRow, owner: owner, location: location, work: work}
	n.self = n
	defer func() {
		if err != nil {
			err = errors.Join(err, n.Close())
		}
	}()
	switch location.Location.Kind {
	case "absolute":
		n.project, err = OpenProjectRoot(string(location.Location.PathBytes))
		if err != nil {
			return nil, err
		}
		if n.project.dir != string(location.Location.PathBytes) {
			return nil, errDialectArtifactOwner
		}
		n.root = n.project.own.root
	case "work":
		n.root, n.edges, err = openDialectArtifactSelectedRelativeProject(ctx, work.root, string(location.Location.PathBytes))
	case "home":
		homeRow, readErr := ownerRow.metadata.homeRow(ctx, location.Location.Anchor)
		if readErr != nil {
			return nil, readErr
		}
		n.home, err = openDialectArtifactSelectedSourceHome(ctx, homeRow, work, string(location.Location.PathBytes))
		if err == nil {
			n.root, n.edges, err = openDialectArtifactSelectedRelativeProject(ctx, n.home.root, string(location.Location.PathBytes))
		}
	default:
		return nil, errDialectArtifactOwner
	}
	if err != nil {
		return nil, err
	}
	n.info, err = n.root.Stat(".")
	if err != nil || identityKey(n.info) != owner.FolderIdentity {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	private, err := openDialectArtifactOwnerFolder(ctx, n.root, false, "selected-native-private", ".desk-private")
	if err != nil {
		return nil, err
	}
	n.privateEdges, err = pinInjectedArtifactHeldEdges(ctx, n.root, private, ".desk-private")
	var markerFile *os.File
	if err == nil {
		markerFile, err = openDialectArtifactOwnerFile(private, dialectArtifactOwnerMarker)
	}
	if err == nil {
		n.markerInfo, err = markerFile.Stat()
		if err == nil {
			n.markerRaw, err = readBounded(markerFile, dialectArtifactOwnerMarkerBytes)
		}
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-native-marker", markerFile.Close))
	}
	err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-native-private", private.Close))
	if err != nil {
		return nil, err
	}
	marker, err := readDialectArtifactOwnerMarker(n.markerRaw)
	if err != nil || marker.Owner != owner.Owner {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	if err = n.check(ctx, false); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *dialectArtifactSelectedNativeSource) check(ctx context.Context, requireWork bool) (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(errDialectArtifactOwner, err)
		}
	}()
	if n == nil || n.self != n || n.closed || n.root == nil || n.work == nil || ctx == nil {
		return errDialectArtifactOwner
	}
	if err = n.ownerRow.check(ctx); err != nil {
		return err
	}
	if err = n.locationRow.check(ctx); err != nil {
		return err
	}
	if err = n.work.check(ctx, requireWork); err != nil {
		return err
	}
	info, err := n.root.Stat(".")
	if err != nil || !os.SameFile(info, n.info) || identityKey(info) != n.owner.FolderIdentity {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if n.project != nil {
		current, pathErr := os.Lstat(n.project.dir)
		if pathErr != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, n.info) {
			return errors.Join(errDialectArtifactOwner, pathErr)
		}
	}
	if n.home != nil {
		if err = n.home.check(ctx, requireWork); err != nil {
			return err
		}
	}
	if err = checkDialectArtifactSelectedProjectEdges(ctx, n.edges); err != nil {
		return err
	}
	if err = checkInjectedArtifactEdges(ctx, n.privateEdges); err != nil {
		return err
	}
	private, err := openDialectArtifactOwnerFolder(ctx, n.root, false, "selected-native-marker-check", ".desk-private")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-native-marker-check", private.Close))
	}()
	file, err := openDialectArtifactOwnerFile(private, dialectArtifactOwnerMarker)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "selected-native-marker-check-file", file.Close))
	}()
	held, statErr := file.Stat()
	again, lookErr := private.Lstat(dialectArtifactOwnerMarker)
	if statErr != nil || lookErr != nil || !os.SameFile(held, n.markerInfo) || !os.SameFile(again, n.markerInfo) {
		return errors.Join(errDialectArtifactOwner, statErr, lookErr)
	}
	raw, err := readBounded(file, dialectArtifactOwnerMarkerBytes)
	if err != nil || !bytes.Equal(raw, n.markerRaw) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return nil
}

func (n *dialectArtifactSelectedNativeSource) namespace(ctx context.Context, data *os.Root, scope dialectArtifactOwnerScope) error {
	if err := n.check(ctx, true); err != nil {
		return err
	}
	if !scope.valid() || scope.Owner != n.owner.Owner {
		return errDialectArtifactOwner
	}
	if n.home != nil && scope.Kind == "native" {
		actual, err := assistantRunNamespace(data, n.home.entry.HistoryIdentity)
		if err != nil || actual != scope.Namespace {
			return errors.Join(errDialectArtifactOwner, err)
		}
	}
	return nil
}

func (n *dialectArtifactSelectedNativeSource) Close() error {
	if n == nil {
		return nil
	}
	if n.self != n {
		return errDialectArtifactOwner
	}
	if n.closed {
		return n.closeErr
	}
	n.closed = true
	if n.project != nil {
		n.closeErr = errors.Join(n.closeErr, closeDialectArtifactOwner(n.ctx, "selected-native-project", n.project.Close))
	} else if n.root != nil {
		n.closeErr = errors.Join(n.closeErr, closeDialectArtifactOwner(n.ctx, "selected-native-project", n.root.Close))
	}
	n.closeErr = errors.Join(n.closeErr, n.home.close())
	return n.closeErr
}
