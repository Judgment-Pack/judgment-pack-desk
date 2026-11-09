package desk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type homeMoveProjectHold struct {
	selected    dialectArtifactHomeMoveProject
	root        *ProjectRoot
	lock        *os.File
	private     *os.Root
	marker      *os.File
	ownerFolder *os.Root
	owner       *os.File
}
type homeMoveQueueHold struct {
	root *os.Root
	file *os.File
}
type homeMoveParentHold struct {
	selected dialectArtifactHomeMoveParent
	root     *ProjectRoot
}
type dialectArtifactHomeMoveHold struct {
	ctx                             context.Context
	tx                              *deskHomeTransaction
	plan                            *dialectArtifactHomeMovePlan
	config, sourceHome, destination *ProjectRoot
	lease, work                     *os.File
	leaseOwned                      bool
	private                         io.Closer
	releaseWrites                   func()
	secrets, metadata               *os.Root
	projects                        []*homeMoveProjectHold
	parents                         []homeMoveParentHold
	queues                          []homeMoveQueueHold
	providers                       *dialectArtifactSelectedProviderPins
	edges                           []retainedArtifactInjectedEdge
	journal                         *ProjectRoot
	journalName                     string
	journalRaw                      []byte
	journalInfo                     os.FileInfo
	ready, closed, innerClosed      bool
	closeErr                        error
	seenAfter                       map[string]bool
}

func currentHomeMovePath(tx *deskHomeTransaction, path, identity string) (string, error) {
	candidates := []string{path}
	current := path
	for _, op := range tx.Renames {
		if pathContains(op.From, current) {
			relative, e := filepath.Rel(op.From, current)
			if e != nil {
				return "", e
			}
			current = filepath.Join(op.To, relative)
			candidates = append(candidates, current)
		}
	}
	for _, candidate := range candidates {
		info, e := os.Lstat(candidate)
		if e == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && identityKey(info) == identity {
			return candidate, nil
		}
	}
	return "", errDialectArtifactOwner
}

// A parent's own recorded inode is mandatory even when its renamed ancestor
// is intact. Mapping alone cannot distinguish a replaced descendant directory.
func currentHomeMoveParentPath(tx *deskHomeTransaction, pin dialectArtifactHomeMoveParent) (string, error) {
	path := string(pin.Path)
	for _, op := range tx.Renames {
		if pathContains(op.From, path) {
			root, err := currentHomeMovePath(tx, op.From, op.Identity)
			if err != nil {
				return "", err
			}
			relative, err := filepath.Rel(op.From, path)
			if err != nil {
				return "", err
			}
			path = filepath.Join(root, relative)
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || identityKey(info) != pin.Identity {
		return "", errors.Join(errDialectArtifactOwner, err)
	}
	return path, nil
}
func (h *dialectArtifactHomeMoveHold) checkRenameParents() error {
	for _, parent := range h.parents {
		path, err := currentHomeMoveParentPath(h.tx, parent.selected)
		if err != nil || !sameDirectory(path, parent.root.info) || identityKey(parent.root.info) != parent.selected.Identity {
			return errors.Join(errDialectArtifactOwner, err)
		}
	}
	return nil
}
func (h *dialectArtifactHomeMoveHold) renameParent(i int) (*ProjectRoot, error) {
	path := homeMoveOriginalParent(h.tx.Renames, i)
	for _, parent := range h.parents {
		if string(parent.selected.Path) == path {
			return parent.root, h.checkRenameParents()
		}
	}
	return nil, errDialectArtifactOwner
}
func openHomeMoveRoot(tx *deskHomeTransaction, path, identity string) (*ProjectRoot, error) {
	current, err := currentHomeMovePath(tx, path, identity)
	if err != nil {
		return nil, err
	}
	root, err := OpenProjectRoot(current)
	if err != nil {
		return nil, err
	}
	if root.dir != current || identityKey(root.info) != identity {
		return nil, errors.Join(errDialectArtifactOwner, root.Close())
	}
	return root, nil
}

// Live construction borrows the already upgraded original home lease. Replay
// instead acquires the journaled existing lease, before any project/work lock.
// Neither branch opens a provider from a persisted path or constructs a Server.
func openDialectArtifactHomeMoveHold(ctx context.Context, tx *deskHomeTransaction, p *dialectArtifactHomeMovePlan, live *Server, lease *os.File, providers *dialectArtifactSelectedProviderPins) (h *dialectArtifactHomeMoveHold, err error) {
	h = &dialectArtifactHomeMoveHold{ctx: ctx, tx: tx, plan: p, providers: providers, seenAfter: map[string]bool{}}
	owned := h
	defer func() {
		if err != nil {
			err = errors.Join(err, owned.Close())
			h = nil
		}
	}()
	h.config, err = openHomeMoveRoot(tx, tx.Config, p.ConfigIdentity)
	if err != nil {
		return nil, err
	}
	if lease != nil {
		h.lease = lease
	} else {
		h.lease, err = lockDialectArtifactExistingMove(h.config.own.root, ".home-use.lock")
		h.leaseOwned = true
		if err != nil {
			return nil, err
		}
	}
	if p.SourceHome != "" {
		h.sourceHome, err = openHomeMoveRoot(tx, string(p.SourceHome), p.SourceHomeIdentity)
		if err != nil {
			return nil, err
		}
	} else if p.DestinationIdentity != "" {
		h.destination, err = openHomeMoveRoot(tx, tx.Destination, p.DestinationIdentity)
		if err != nil {
			return nil, err
		}
	}
	projects := append([]dialectArtifactHomeMoveProject(nil), p.Projects...)
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Identity+":"+projects[i].ID < projects[j].Identity+":"+projects[j].ID
	})
	for _, selected := range projects {
		project := &homeMoveProjectHold{selected: selected}
		h.projects = append(h.projects, project)
		project.root, err = openHomeMoveRoot(tx, string(selected.From), selected.Identity)
		if err != nil {
			return nil, err
		}
		project.lock, err = project.root.own.root.Open(".")
		if err != nil {
			return nil, err
		}
		if err = lockProjectFile(project.lock); err != nil {
			return nil, err
		}
	}
	for _, pin := range p.RenameParents {
		path, e := currentHomeMoveParentPath(tx, pin)
		if e != nil {
			return nil, e
		}
		root, e := OpenProjectRoot(path)
		if e != nil {
			return nil, e
		}
		h.parents = append(h.parents, homeMoveParentHold{pin, root})
		if root.dir != path || identityKey(root.info) != pin.Identity {
			return nil, errDialectArtifactOwner
		}
	}
	h.work, err = lockDialectArtifactExistingMove(h.config.own.root, ".data-work.lock")
	if err != nil {
		return nil, err
	}
	if live != nil {
		if !live.writes.TryLock() {
			return nil, errRetainedArtifactBusy
		}
		h.releaseWrites = live.writes.Unlock
	}
	h.private, err = lockDialectArtifactExistingMove(h.config.own.root, ".data.lock")
	if err != nil {
		return nil, err
	}
	h.secrets, h.metadata, err = openHomeMoveMetadata(ctx, h.config.own.root)
	if err != nil {
		return nil, err
	}
	for _, edge := range []struct {
		from, to *os.Root
		parts    []string
	}{{h.config.own.root, h.secrets, []string{secretsDirName}}, {h.secrets, h.metadata, []string{dialectArtifactOwnersFolder}}} {
		pins, e := pinInjectedArtifactHeldEdges(ctx, edge.from, edge.to, edge.parts...)
		if e != nil {
			return nil, e
		}
		h.edges = append(h.edges, pins...)
	}
	byOwner := append([]*homeMoveProjectHold(nil), h.projects...)
	sort.Slice(byOwner, func(i, j int) bool { return byOwner[i].selected.Owner < byOwner[j].selected.Owner })
	for _, project := range byOwner {
		if project.selected.Owner == "" {
			continue
		}
		project.private, err = openDialectArtifactOwnerFolder(ctx, project.root.own.root, false, "home-move-private", ".desk-private")
		if err != nil {
			return nil, err
		}
		pins, e := pinInjectedArtifactHeldEdges(ctx, project.root.own.root, project.private, ".desk-private")
		if e != nil {
			return nil, e
		}
		h.edges = append(h.edges, pins...)
		project.marker, err = lockDialectArtifactExistingMove(project.private, ".dialect-artifact-owner.lock")
		if err != nil {
			return nil, err
		}
		owner := project.selected.Owner
		project.ownerFolder, err = openDialectArtifactOwnerFolder(ctx, h.metadata, false, "home-move-owner-locks", "locks", owner[:2])
		if err != nil {
			return nil, err
		}
		pins, e = pinInjectedArtifactHeldEdges(ctx, h.metadata, project.ownerFolder, "locks", owner[:2])
		if e != nil {
			return nil, e
		}
		h.edges = append(h.edges, pins...)
		project.owner, err = lockDialectArtifactExistingMove(project.ownerFolder, owner[2:]+".lock")
		if err != nil {
			return nil, err
		}
	}
	// Native queues are ordered by selected owner. Injected original queues are
	// borrowed only in the live branch; replay changes no provider binding/data.
	for _, project := range byOwner {
		if project.selected.Kind != "file" || project.selected.ArtifactsIdentity == "" {
			continue
		}
		root, e := openRetainedArtifactsWithin(project.root.own.root, false)
		if e != nil {
			return nil, e
		}
		h.queues = append(h.queues, homeMoveQueueHold{root: root})
		queue := &h.queues[len(h.queues)-1]
		fi, e := root.Stat(".")
		if e != nil || identityKey(fi) != project.selected.ArtifactsIdentity {
			return nil, errors.Join(errDialectArtifactOwner, e)
		}
		queue.file, err = root.Open(".")
		if err != nil {
			return nil, err
		}
		if err = lockPrivateData(queue.file, true); err != nil {
			return nil, err
		}
		pins, e := pinInjectedArtifactHeldEdges(ctx, project.root.own.root, root, ".desk-private", "retained-artifacts")
		if e != nil {
			return nil, e
		}
		h.edges = append(h.edges, pins...)
	}
	if providers != nil {
		for _, provider := range providers.ordered {
			root, e := provider.registration.root.OpenRoot(".")
			if e != nil {
				return nil, e
			}
			h.queues = append(h.queues, homeMoveQueueHold{root: root})
			queue := &h.queues[len(h.queues)-1]
			queue.file, err = root.Open(".")
			if err != nil {
				return nil, err
			}
			if err = lockPrivateData(queue.file, true); err != nil {
				return nil, err
			}
		}
	}
	h.ready = true
	if err = h.check(); err != nil {
		return nil, err
	}
	return h, nil
}
func sameHomeMoveFile(root *os.Root, name, identity string, file *os.File) error {
	info, err := root.Lstat(name)
	if err != nil || identityKey(info) != identity || ownerOnlyFile(name, info.Mode()) != nil || ownedByUs(name, info) != nil {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if file != nil {
		held, e := file.Stat()
		if e != nil || !os.SameFile(info, held) {
			return errors.Join(errDialectArtifactOwner, e)
		}
	}
	return nil
}
func (h *dialectArtifactHomeMoveHold) check() error {
	if h == nil || h.closed || !h.ready || h.config == nil || h.plan == nil {
		return errDialectArtifactOwner
	}
	if err := h.checkOuter(); err != nil {
		return err
	}
	if err := h.checkRenameParents(); err != nil {
		return err
	}
	p := h.plan
	if _, err := currentHomeMovePath(h.tx, h.tx.Config, p.ConfigIdentity); err != nil {
		return err
	}
	for _, pin := range []struct {
		root     *os.Root
		identity string
	}{{h.config.own.root, p.ConfigIdentity}, {h.secrets, p.SecretsIdentity}, {h.metadata, p.MetadataIdentity}} {
		fi, e := pin.root.Stat(".")
		if e != nil || identityKey(fi) != pin.identity {
			return errors.Join(errDialectArtifactOwner, e)
		}
	}
	if err := sameHomeMoveFile(h.config.own.root, ".home-use.lock", p.LeaseIdentity, h.lease); err != nil {
		return err
	}
	if err := sameHomeMoveFile(h.config.own.root, ".data-work.lock", p.WorkLockIdentity, h.work); err != nil {
		return err
	}
	if err := sameHomeMoveFile(h.config.own.root, ".data.lock", p.PrivateLockIdentity, nil); err != nil {
		return err
	}
	if p.SourceHome != "" {
		if _, err := currentHomeMovePath(h.tx, string(p.SourceHome), p.SourceHomeIdentity); err != nil {
			return err
		}
	} else if p.DestinationIdentity != "" {
		if _, err := currentHomeMovePath(h.tx, h.tx.Destination, p.DestinationIdentity); err != nil {
			return err
		}
	}
	if err := checkInjectedArtifactEdges(h.ctx, h.edges); err != nil {
		return err
	}
	if p.SourceHome != "" {
		raw, info, err := readHomeMoveFile(h.config.own.root, deskHomeRegistryName, 1<<20)
		if err != nil || identityKey(info) != p.RegistryIdentity || digestOf(raw) != p.RegistrySHA256 {
			return errors.Join(errDialectArtifactOwner, err)
		}
	} else {
		raw, _, err := readHomeMoveFile(h.config.own.root, deskHomeRegistryName, 1<<20)
		if !injectedArtifactOnlyNotExist(err) && (err != nil || p.RegistryAfterSHA256 == "" || digestOf(raw) != p.RegistryAfterSHA256) {
			return errors.Join(errDialectArtifactOwner, err)
		}
	}
	for _, project := range h.projects {
		if _, err := currentHomeMovePath(h.tx, string(project.selected.From), project.selected.Identity); err != nil {
			return err
		}
		fi, err := project.root.own.root.Stat(".")
		if err != nil || identityKey(fi) != project.selected.Identity {
			return errors.Join(errDialectArtifactOwner, err)
		}
		if project.selected.Owner == "" {
			if project.selected.PrivateIdentity != "" {
				fi, e := project.root.own.root.Lstat(".desk-private")
				if e != nil || identityKey(fi) != project.selected.PrivateIdentity {
					return errors.Join(errDialectArtifactOwner, e)
				}
			}
			_, e := project.root.own.root.Lstat(filepath.Join(".desk-private", dialectArtifactOwnerMarker))
			if injectedArtifactOnlyNotExist(e) {
				continue
			}
			return errors.Join(errDialectArtifactOwner, e)
		}
		private, err := openDialectArtifactOwnerFolder(h.ctx, project.root.own.root, false, "home-move-marker-check", ".desk-private")
		if injectedArtifactOnlyNotExist(err) && project.selected.PrivateIdentity == "" && project.selected.Owner == "" {
			continue
		}
		if err != nil {
			return err
		}
		checkErr := func() error {
			if project.selected.PrivateIdentity != "" {
				fi, e := private.Stat(".")
				if e != nil || identityKey(fi) != project.selected.PrivateIdentity {
					return errors.Join(errDialectArtifactOwner, e)
				}
			}
			raw, info, e := readHomeMoveFile(private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
			if project.selected.Owner == "" {
				if injectedArtifactOnlyNotExist(e) {
					return nil
				}
				return errors.Join(errDialectArtifactOwner, e)
			}
			if e != nil || identityKey(info) != project.selected.MarkerIdentity || digestOf(raw) != project.selected.MarkerSHA256 {
				return errors.Join(errDialectArtifactOwner, e)
			}
			if e = sameHomeMoveFile(private, ".dialect-artifact-owner.lock", project.selected.MarkerLockIdentity, project.marker); e != nil {
				return e
			}
			held, e := project.owner.Stat()
			if e != nil {
				return e
			}
			if identityKey(held) != project.selected.OwnerLockIdentity {
				return errDialectArtifactOwner
			}
			return sameHomeMoveFile(project.ownerFolder, project.selected.Owner[2:]+".lock", project.selected.OwnerLockIdentity, project.owner)
		}()
		checkErr = errors.Join(checkErr, closeDialectArtifactOwner(h.ctx, "home-move-marker-check", private.Close))
		if checkErr != nil {
			return checkErr
		}
	}
	for _, pin := range p.Pins {
		raw, info, folder, err := readHomeMoveRow(h.ctx, h.metadata, pin.Table, pin.ID, pin.Namespace)
		if err != nil || identityKey(info) != pin.Identity || folder != pin.FolderIdentity || digestOf(raw) != pin.SHA256 {
			return errors.Join(errDialectArtifactOwner, err)
		}
		for _, project := range p.Projects {
			if project.Owner != pin.ID {
				continue
			}
			if pin.Table == "owners" {
				if project.Kind == "file" {
					v, e := readDialectArtifactOwnerRecord(raw)
					if e != nil || v.Owner != pin.ID || v.FolderIdentity != project.Identity || v.Work.ID != p.WorkDomain || v.Work.FolderIdentity != p.ConfigIdentity {
						return errors.Join(errDialectArtifactOwner, e)
					}
				} else {
					v, e := readDialectArtifactInjectedOwnerRecord(raw)
					if e != nil || v.Owner != pin.ID || v.ProjectFolderIdentity != project.Identity || v.WorkDomain != p.WorkDomain || v.Provider != project.Provider {
						return errors.Join(errDialectArtifactOwner, e)
					}
				}
			} else {
				v, e := readDialectArtifactOwnerScope(raw)
				if e != nil || v.Kind != "native" || v.Owner != project.Owner || v.Namespace != project.Namespace {
					return errors.Join(errDialectArtifactOwner, e)
				}
			}
		}
	}

	for _, row := range p.Rows {
		if err := h.checkRow(row); err != nil {
			return err
		}
	}
	if h.providers != nil {
		if err := h.providers.check(h.ctx); err != nil {
			return err
		}
	}
	for _, queue := range h.queues {
		held, e := queue.file.Stat()
		current, e2 := queue.root.Stat(".")
		if e != nil || e2 != nil || !os.SameFile(held, current) {
			return errors.Join(errDialectArtifactOwner, e, e2)
		}
	}
	if h.journal != nil {
		if !sameDirectory(h.journal.dir, h.journal.info) {
			return errDialectArtifactOwner
		}
		raw, info, e := readHomeMoveFile(h.journal.own.root, h.journalName, homeTransactionLimit)
		if e != nil || !os.SameFile(info, h.journalInfo) || !bytes.Equal(raw, h.journalRaw) {
			return errors.Join(errDialectArtifactOwner, e)
		}
	}
	return nil
}
func (h *dialectArtifactHomeMoveHold) checkRow(row dialectArtifactHomeMoveRow) error {
	raw, info, folder, err := readHomeMoveRow(h.ctx, h.metadata, row.Table, row.ID, "")
	if injectedArtifactOnlyNotExist(err) && row.BeforeKind == "absent" && !h.seenAfter[row.Table+":"+row.ID] {
		return nil
	}
	if err != nil {
		return err
	}
	if row.FolderIdentity != "" && folder != row.FolderIdentity {
		return errDialectArtifactOwner
	}
	if bytes.Equal(raw, row.Before) && row.BeforeKind == "present" {
		if h.seenAfter[row.Table+":"+row.ID] && !bytes.Equal(row.Before, row.After) {
			return errDialectArtifactOwner
		}
		if identityKey(info) != row.Identity {
			return errDialectArtifactOwner
		}
		return nil
	}
	if len(row.After) > 0 && bytes.Equal(raw, row.After) {
		h.seenAfter[row.Table+":"+row.ID] = true
		return nil
	}
	return errDialectArtifactOwner
}
func (h *dialectArtifactHomeMoveHold) applyRow(row dialectArtifactHomeMoveRow) (err error) {
	if err = h.check(); err != nil {
		return err
	}
	parts, name, _, ok := homeMoveRowKey(row.Table, row.ID, "")
	if !ok {
		return errDialectArtifactOwner
	}
	folder, err := openDialectArtifactOwnerFolder(h.ctx, h.metadata, row.BeforeKind == "absent", "home-move-cas-folder", parts...)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-move-cas-folder", folder.Close)) }()
	raw, observed, readErr := readHomeMoveFile(folder, name, len(row.After)+len(row.Before))
	if readErr == nil && bytes.Equal(raw, row.After) {
		// The prior rename may have landed without a durable directory entry.
		// Acknowledge that same inode and exact bytes under the original pins.
		check := func() error {
			current, e := folder.Lstat(name)
			if e != nil || !os.SameFile(observed, current) {
				return errors.Join(errDialectArtifactOwner, e)
			}
			return h.check()
		}
		if err = check(); err != nil {
			return err
		}
		if err = syncExistingDialectArtifactOwner(h.ctx, folder, name, row.After, "home-move-row"); err != nil {
			return err
		}
		return acknowledgeDialectArtifactOwner(h.ctx, folder, name, row.After, "home-move-row", check)
	}
	if row.BeforeKind == "absent" {
		if !injectedArtifactOnlyNotExist(readErr) {
			return errors.Join(errDialectArtifactOwner, readErr)
		}
		return claimDialectArtifactOwner(h.ctx, folder, name, row.After, "home-move-row", h.check)
	}
	if readErr != nil || !bytes.Equal(raw, row.Before) {
		return errors.Join(errDialectArtifactOwner, readErr)
	}
	file, stage, err := newDataStage(folder)
	if err != nil {
		return err
	}
	defer folder.Remove(stage)
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-move-stage", file.Close))
		}
	}()
	if _, err = file.Write(row.After); err != nil {
		return err
	}
	if err = file.Chmod(custodyFileMode); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	staged, err := file.Stat()
	if err != nil {
		return err
	}
	closed = true
	if err = closeDialectArtifactOwner(h.ctx, "home-move-stage", file.Close); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(h.ctx, "home-move-row.before-cas"); err != nil {
		return err
	}
	if err = h.check(); err != nil {
		return err
	}
	if err = folder.Rename(stage, name); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(h.ctx, "home-move-row.before-directory-sync"); err != nil {
		return err
	}
	if err = syncDialectArtifactOwnerDirectory(folder); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(h.ctx, "home-move-row.after-cas"); err != nil {
		return err
	}
	after, info, err := readHomeMoveFile(folder, name, len(row.After))
	if err != nil || !bytes.Equal(after, row.After) || !os.SameFile(staged, info) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return h.check()
}
func (h *dialectArtifactHomeMoveHold) checkOuter() error {
	if err := h.ctx.Err(); err != nil {
		return err
	}
	if _, err := currentHomeMovePath(h.tx, h.tx.Config, h.plan.ConfigIdentity); err != nil {
		return err
	}
	if err := sameHomeMoveFile(h.config.own.root, ".home-use.lock", h.plan.LeaseIdentity, h.lease); err != nil {
		return err
	}
	if h.tx.Parent != "" {
		fi, err := os.Lstat(h.tx.Parent)
		if err != nil || identityKey(fi) != h.tx.ParentIdentity {
			return errors.Join(errDialectArtifactOwner, err)
		}
	}
	if h.plan.SourceHome != "" {
		if _, err := currentHomeMovePath(h.tx, string(h.plan.SourceHome), h.plan.SourceHomeIdentity); err != nil {
			return err
		}
	} else if h.plan.DestinationIdentity != "" {
		if _, err := currentHomeMovePath(h.tx, h.tx.Destination, h.plan.DestinationIdentity); err != nil {
			return err
		}
	}
	if h.journal != nil {
		raw, info, err := readHomeMoveFile(h.journal.own.root, h.journalName, homeTransactionLimit)
		if err != nil || !os.SameFile(info, h.journalInfo) || !bytes.Equal(raw, h.journalRaw) {
			return errors.Join(errDialectArtifactOwner, err)
		}
	}
	return nil
}
func (h *dialectArtifactHomeMoveHold) CloseInner() error {
	if h.innerClosed {
		return h.closeErr
	}
	if h.ready {
		h.closeErr = errors.Join(h.closeErr, h.check())
	}
	h.innerClosed = true
	closeOne := func(label string, f func() error) {
		h.closeErr = errors.Join(h.closeErr, closeDialectArtifactOwner(h.ctx, label, f))
	}
	for i := len(h.queues) - 1; i >= 0; i-- {
		q := h.queues[i]
		if q.file != nil {
			closeOne("home-move-queue", q.file.Close)
		}
		if q.root != nil {
			closeOne("home-move-queue-root", q.root.Close)
		}
	}
	for i := len(h.parents) - 1; i >= 0; i-- {
		closeOne("home-move-rename-parent", h.parents[i].root.Close)
	}
	for i := len(h.projects) - 1; i >= 0; i-- {
		p := h.projects[i]
		if p.owner != nil {
			closeOne("home-move-owner", p.owner.Close)
		}
		if p.ownerFolder != nil {
			closeOne("home-move-owner-folder", p.ownerFolder.Close)
		}
		if p.marker != nil {
			closeOne("home-move-marker", p.marker.Close)
		}
		if p.private != nil {
			closeOne("home-move-private", p.private.Close)
		}
	}
	if h.metadata != nil {
		closeOne("home-move-metadata", h.metadata.Close)
	}
	if h.secrets != nil {
		closeOne("home-move-secrets", h.secrets.Close)
	}
	if h.private != nil {
		closeOne("home-move-private-lock", h.private.Close)
	}
	if h.releaseWrites != nil {
		h.releaseWrites()
	}
	if h.work != nil {
		closeOne("home-move-work", h.work.Close)
	}
	for i := len(h.projects) - 1; i >= 0; i-- {
		p := h.projects[i]
		if p.lock != nil {
			closeOne("home-move-project-lock", p.lock.Close)
		}
		if p.root != nil {
			closeOne("home-move-project-root", p.root.Close)
		}
	}
	return h.closeErr
}
func (h *dialectArtifactHomeMoveHold) Close() error {
	if h == nil {
		return nil
	}
	if h.closed {
		return h.closeErr
	}
	h.closeErr = errors.Join(h.closeErr, h.CloseInner())
	if h.ready {
		h.closeErr = errors.Join(h.closeErr, h.checkOuter())
	}
	h.closed = true
	closeOne := func(label string, f func() error) {
		h.closeErr = errors.Join(h.closeErr, closeDialectArtifactOwner(h.ctx, label, f))
	}
	if h.destination != nil {
		closeOne("home-move-destination", h.destination.Close)
	}
	if h.sourceHome != nil {
		closeOne("home-move-source-home", h.sourceHome.Close)
	}
	if h.config != nil {
		closeOne("home-move-config", h.config.Close)
	}
	if h.leaseOwned && h.lease != nil {
		closeOne("home-move-lease", h.lease.Close)
	}
	if h.journal != nil {
		closeOne("home-move-journal-root", h.journal.Close)
	}
	return h.closeErr
}

func applyRetainedHomeTransaction(path string, tx *deskHomeTransaction) (err error) {
	if !tx.Retained.valid(tx) {
		return errDialectArtifactOwner
	}
	h, err := openDialectArtifactHomeMoveHold(context.Background(), tx, tx.Retained, nil, nil, nil)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, h.Close()) }()
	h.journal, err = OpenProjectRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	h.journalName = filepath.Base(path)
	h.journalRaw, h.journalInfo, err = readHomeMoveFile(h.journal.own.root, h.journalName, homeTransactionLimit)
	if err != nil {
		return err
	}
	current, e := decodeHomeTransaction(h.journalRaw)
	if e != nil {
		return e
	}
	expected, _ := marshalHomeTransaction(*tx)
	actual, _ := marshalHomeTransaction(*current)
	if !bytes.Equal(expected, actual) {
		return errDialectArtifactOwner
	}
	if err = h.check(); err != nil {
		return err
	}
	if err = applyHomeTransactionHeld(path, tx, h); err != nil {
		return err
	}
	// Close every retained participant before recording completed recovery.
	if err = h.Close(); err != nil {
		return err
	}
	tx.Complete = true
	raw, e := marshalHomeTransaction(*tx)
	if e != nil {
		return e
	}
	// Reopen only the original journal directory after all participants have
	// closed. Publish through that pinned root, never a newly resolved writer.
	root, err := OpenProjectRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, root.Close(), dialectArtifactOwnerStep(context.Background(), "home-move-journal.root-close"))
	}()
	if !os.SameFile(root.info, h.journal.info) {
		return errDialectArtifactOwner
	}
	if err = checkHomeMoveAcknowledgement(root, path, h.journalRaw, h.journalInfo); err != nil {
		return err
	}
	if err = writePrivateData(root.own.root, filepath.Base(path), raw); err != nil {
		return err
	}
	after, info, err := readHomeMoveFile(root.own.root, filepath.Base(path), homeTransactionLimit)
	if err != nil || !bytes.Equal(after, raw) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return acknowledgeHomeMoveFile(root, path, raw, info, "home-move-journal", func() error { return nil })
}

// Acknowledge only the original owned file, exact bytes and current directory
// edge. This is deliberately read-only and does not acquire the whole-home lease.
func checkHomeMoveAcknowledgement(root *ProjectRoot, path string, raw []byte, info os.FileInfo) error {
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || root.dir != filepath.Dir(path) || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || !os.SameFile(parent, root.info) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	after, current, err := readHomeMoveFile(root.own.root, filepath.Base(path), homeTransactionLimit)
	if err != nil || !os.SameFile(info, current) || !bytes.Equal(after, raw) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return nil
}

func acknowledgeHomeMoveFile(root *ProjectRoot, path string, raw []byte, info os.FileInfo, label string, check func() error) (err error) {
	ctx := context.Background()
	verify := func() error {
		if err := checkHomeMoveAcknowledgement(root, path, raw, info); err != nil {
			return err
		}
		return check()
	}
	if err = verify(); err != nil {
		return err
	}
	file, err := openDialectArtifactOwnerFile(root.own.root, filepath.Base(path))
	if err != nil {
		return err
	}
	closed := false
	closeFile := func() error {
		closed = true
		return errors.Join(file.Close(), dialectArtifactOwnerStep(ctx, label+".file-close"))
	}
	defer func() {
		if !closed {
			err = errors.Join(err, closeFile())
		}
	}()
	held, err := file.Stat()
	if err != nil || !os.SameFile(held, info) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".sync"); err != nil {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	if err = closeFile(); err != nil {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	if err = root.own.dirFile.Sync(); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".directory-sync"); err != nil {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, label+".readback"); err != nil {
		return err
	}
	return verify()
}

func applyHomeWriteAcknowledged(write homeWrite, check func() error) (err error) {
	if err = check(); err != nil {
		return err
	}
	if err = applyHomeWrite(write); err != nil {
		return err
	}
	root, err := OpenProjectRoot(filepath.Dir(write.Path))
	if err != nil {
		return err
	}
	label := ""
	if !write.Executable && filepath.Base(write.Path) == deskHomePointerName {
		label = "home-move-pointer"
	}
	defer func() {
		closeErr := root.Close()
		if label != "" {
			closeErr = errors.Join(closeErr, dialectArtifactOwnerStep(context.Background(), label+".root-close"))
		}
		err = errors.Join(err, closeErr)
	}()
	if label != "" {
		raw, info, e := readHomeMoveFile(root.own.root, filepath.Base(write.Path), homeTransactionLimit)
		if e != nil || !bytes.Equal(raw, write.Data) {
			return errors.Join(errDialectArtifactOwner, e)
		}
		return acknowledgeHomeMoveFile(root, write.Path, raw, info, label, check)
	}
	var raw []byte
	if write.Executable {
		raw, err = readHomeLauncher(write.Path)
	} else {
		raw, err = readHomeMetadataIn(root.own.root, filepath.Base(write.Path), homeTransactionLimit)
	}
	if err != nil || !bytes.Equal(raw, write.Data) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	file, err := root.own.root.OpenFile(filepath.Base(write.Path), os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	syncErr = errors.Join(syncErr, file.Close())
	if syncErr != nil {
		return syncErr
	}
	if err = root.own.dirFile.Sync(); err != nil {
		return err
	}
	if !sameDirectory(filepath.Dir(write.Path), root.info) {
		return errDialectArtifactOwner
	}
	return check()
}

func (s *Server) holdUnretainedHomeMove(ctx context.Context, plan deskHomePlan) (close func() error, err error) {
	files := []*os.File{}
	roots := []*ProjectRoot{}
	var private *privateDataShortLock
	writes := false
	cleanup := func() error {
		var result error
		if private != nil {
			result = errors.Join(result, private.Close())
		}
		if writes {
			s.writes.Unlock()
		}
		for i := len(files) - 1; i >= 0; i-- {
			result = errors.Join(result, files[i].Close())
		}
		for i := len(roots) - 1; i >= 0; i-- {
			result = errors.Join(result, roots[i].Close())
		}
		return result
	}
	close = cleanup
	defer func() {
		if err != nil {
			err = errors.Join(err, cleanup())
		}
	}()
	paths := []string{}
	for _, entry := range plan.Registry.Desks {
		paths = append(paths, homeMoveProjectPath(s, entry))
	}
	sort.Strings(paths)
	for _, path := range paths {
		root, e := OpenProjectRoot(path)
		if e != nil {
			return nil, e
		}
		roots = append(roots, root)
		file, e := root.own.root.Open(".")
		if e != nil {
			return nil, e
		}
		files = append(files, file)
		if e = lockProjectFile(file); e != nil {
			return nil, e
		}
	}
	work, e := s.openPrivateDataFileLock(ctx, ".data-work.lock", true, false)
	if e != nil {
		return nil, e
	}
	files = append(files, work)
	if !s.writes.TryLock() {
		return nil, errRetainedArtifactBusy
	}
	writes = true
	private, err = s.tryPrivateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	return close, nil
}
