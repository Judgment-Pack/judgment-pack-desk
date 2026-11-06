package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func readHomeMoveRow(ctx context.Context, metadata *os.Root, table, id, namespace string) (raw []byte, info os.FileInfo, folderIdentity string, err error) {
	parts, name, limit, ok := homeMoveRowKey(table, id, namespace)
	if !ok {
		return nil, nil, "", errDialectArtifactOwner
	}
	folder, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "home-move-row", parts...)
	if err != nil {
		return nil, nil, "", err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-move-row", folder.Close)) }()
	fi, err := folder.Stat(".")
	if err != nil {
		return nil, nil, "", err
	}
	folderIdentity = identityKey(fi)
	file, err := openDialectArtifactOwnerFile(folder, name)
	if err != nil {
		return nil, nil, "", err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-move-row-file", file.Close)) }()
	info, err = file.Stat()
	if err != nil {
		return nil, nil, "", err
	}
	raw, err = readBounded(file, limit)
	again, e := folder.Lstat(name)
	if e != nil || !os.SameFile(info, again) {
		return nil, nil, "", errors.Join(errDialectArtifactOwner, e)
	}
	return
}
func homeMoveMutable(table, id string, raw []byte, info os.FileInfo, folder string) dialectArtifactHomeMoveRow {
	return dialectArtifactHomeMoveRow{Table: table, ID: id, Identity: identityKey(info), FolderIdentity: folder, BeforeKind: "present", Before: raw, BeforeSHA256: digestOf(raw), After: []byte{}, Write: -1}
}
func homeMovePin(table, id, namespace string, raw []byte, info os.FileInfo, folder string) dialectArtifactHomeMovePin {
	return dialectArtifactHomeMovePin{table, id, namespace, identityKey(info), folder, digestOf(raw)}
}
func homeMoveProjectPath(s *Server, entry deskHomeEntry) string {
	if s.home != nil {
		return filepath.Join(s.home.Path, entry.Folder)
	}
	if entry.Kind == "desk" {
		return filepath.Join(s.configDir, "desks", entry.ID)
	}
	return s.projectDir
}

// Pure selected observation for the plan MAC. No enrollment, new directory,
// provider construction, table enumeration or future destination inode exists.
func (s *Server) snapshotDialectArtifactHomeMove(plan deskHomePlan) (out *dialectArtifactHomeMovePlan, err error) {
	ctx := context.Background()
	var metadata, secrets *os.Root
	defer func() {
		if metadata != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-move-snapshot", metadata.Close))
		}
		if secrets != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-move-snapshot-secrets", secrets.Close))
		}
		if err != nil {
			out = nil
		}
	}()
	secrets, metadata, err = openHomeMoveMetadata(ctx, s.assistant.root)
	if injectedArtifactOnlyNotExist(err) {
		metadata = nil
		err = nil
	} else if err != nil {
		return nil, err
	}
	p := &dialectArtifactHomeMovePlan{Version: 2, RegistryOwner: plan.Registry.Owner, RegistrySHA256: "absent", Projects: []dialectArtifactHomeMoveProject{}, Pins: []dialectArtifactHomeMovePin{}, Rows: []dialectArtifactHomeMoveRow{}}
	rootInfo, err := s.assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	p.ConfigIdentity = identityKey(rootInfo)
	if s.home != nil {
		p.SourceHome = homePath(s.home.Path)
		homeInfo, e := os.Lstat(s.home.Path)
		if e != nil || !homeInfo.IsDir() || homeInfo.Mode()&os.ModeSymlink != 0 {
			return nil, errors.Join(errDialectArtifactOwner, e)
		}
		p.SourceHomeIdentity = identityKey(homeInfo)
		raw, fi, e := readHomeMoveFile(s.assistant.root, deskHomeRegistryName, 1<<20)
		if e != nil {
			return nil, e
		}
		registry, e := decodeHomeRegistry(raw)
		if e != nil || registry.Owner != plan.Registry.Owner {
			return nil, errors.Join(errDialectArtifactOwner, e)
		}
		a, _ := json.Marshal(registry)
		b, _ := json.Marshal(plan.Registry)
		if !bytes.Equal(a, b) {
			return nil, errDialectArtifactOwner
		}
		p.RegistryIdentity = identityKey(fi)
		p.RegistrySHA256 = digestOf(raw)
	}
	if metadata != nil {
		metaInfo, e := metadata.Stat(".")
		if e != nil {
			return nil, e
		}
		p.MetadataIdentity = identityKey(metaInfo)
		secretInfo, e := secrets.Stat(".")
		if e != nil {
			return nil, e
		}
		if s.assistant.secrets != nil {
			original, e := s.assistant.secrets.Stat(".")
			if e != nil || !os.SameFile(original, secretInfo) {
				return nil, errors.Join(errDialectArtifactOwner, e)
			}
		}
		p.SecretsIdentity = identityKey(secretInfo)
		for _, pin := range []struct {
			name   string
			target *string
		}{{".home-use.lock", &p.LeaseIdentity}, {".data-work.lock", &p.WorkLockIdentity}, {".data.lock", &p.PrivateLockIdentity}} {
			fi, e := s.assistant.root.Lstat(pin.name)
			if e != nil || ownerOnlyFile(pin.name, fi.Mode()) != nil || ownedByUs(pin.name, fi) != nil {
				return nil, errors.Join(errDialectArtifactOwner, e)
			}
			*pin.target = identityKey(fi)
		}
		p.WorkDomain = dialectArtifactWorkDomainID(p.ConfigIdentity, p.WorkLockIdentity)
		raw, fi, folder, e := readHomeMoveRow(ctx, metadata, "work-domains", p.WorkDomain, "")
		if !injectedArtifactOnlyNotExist(e) {
			if e != nil {
				return nil, e
			}
			v, e := readDialectArtifactWorkDomainRecord(raw)
			if e != nil || v.ID != p.WorkDomain || v.FolderIdentity != p.ConfigIdentity || v.LockIdentity != p.WorkLockIdentity || !bytes.Equal(v.PathBytes, []byte(s.configDir)) {
				return nil, errors.Join(errDialectArtifactOwner, e)
			}
			p.Rows = append(p.Rows, homeMoveMutable("work-domains", p.WorkDomain, raw, fi, folder))
		}
		if s.home != nil {
			id := dialectArtifactHomeAnchorID(p.RegistryOwner, p.SourceHomeIdentity)
			raw, fi, folder, e := readHomeMoveRow(ctx, metadata, "home-anchors", id, "")
			if !injectedArtifactOnlyNotExist(e) {
				if e != nil {
					return nil, e
				}
				v, e := readDialectArtifactHomeAnchorRecord(raw)
				if e != nil || v.ID != id || v.FolderIdentity != p.SourceHomeIdentity || !bytes.Equal(v.PathBytes, []byte(s.home.Path)) {
					return nil, errors.Join(errDialectArtifactOwner, e)
				}
				p.Rows = append(p.Rows, homeMoveMutable("home-anchors", id, raw, fi, folder))
			}
		}
	}
	owners := map[string]bool{}
	for _, entry := range plan.Registry.Desks {
		project, e := OpenProjectRoot(homeMoveProjectPath(s, entry))
		if e != nil {
			return nil, e
		}
		selected, e := snapshotHomeMoveProject(ctx, project, entry, plan, p, metadata)
		e = errors.Join(e, project.Close())
		if e != nil {
			return nil, e
		}
		if selected.Owner != "" {
			if owners[selected.Owner] {
				return nil, errDialectArtifactOwner
			}
			owners[selected.Owner] = true
		}
		p.Projects = append(p.Projects, selected)
	}
	if metadata == nil || len(p.Rows) == 0 {
		if len(owners) > 0 {
			return nil, errDialectArtifactOwner
		}
		return nil, nil
	}
	if p.Rows[0].Table != "work-domains" {
		return nil, errDialectArtifactOwner
	}
	// Predict exactly the existing move and selected ChatFiles rename order.
	// Reverse only preceding moves to select each parent's original path.
	renames := deskHomeTransaction{}
	for _, move := range plan.Moves {
		renames.addRename(move.From, move.To, move.Identity)
	}
	if s.home == nil {
		for _, name := range plan.ChatFiles {
			renames.addRename(filepath.Join(plan.ChatPath, name), filepath.Join(plan.Destination, plan.Registry.Desks[0].Folder, ".desk-private", "data", name), "")
		}
	}
	p.RenameParents = []dialectArtifactHomeMoveParent{}
	for _, path := range homeMoveOriginalParents(renames.Renames) {
		root, e := OpenProjectRoot(path)
		if e != nil {
			return nil, e
		}
		if root.dir != path || !sameDirectory(path, root.info) {
			e = errDialectArtifactOwner
		}
		if e == nil {
			e = safeMoveParent(root.info)
		}
		p.RenameParents = append(p.RenameParents, dialectArtifactHomeMoveParent{homePath(path), identityKey(root.info)})
		e = errors.Join(e, root.Close())
		if e != nil {
			return nil, e
		}
	}
	return p, nil
}

func homeMoveOriginalParent(renames []homeRename, i int) string {
	path := filepath.Dir(renames[i].From)
	for j := i - 1; j >= 0; j-- {
		op := renames[j]
		if pathContains(op.To, path) {
			relative, _ := filepath.Rel(op.To, path)
			path = filepath.Join(op.From, relative)
		}
	}
	return path
}
func homeMoveOriginalParents(renames []homeRename) []string {
	seen := map[string]bool{}
	paths := []string{}
	for i := range renames {
		path := homeMoveOriginalParent(renames, i)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}
func readHomeMoveFile(root *os.Root, name string, limit int) (raw []byte, info os.FileInfo, err error) {
	f, err := openDialectArtifactOwnerFile(root, name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err = f.Stat()
	if err == nil {
		raw, err = readBounded(f, limit)
	}
	return
}
func snapshotHomeMoveProject(ctx context.Context, project *ProjectRoot, entry deskHomeEntry, plan deskHomePlan, p *dialectArtifactHomeMovePlan, metadata *os.Root) (out dialectArtifactHomeMoveProject, err error) {
	out = dialectArtifactHomeMoveProject{ID: entry.ID, Folder: entry.Folder, From: homePath(project.dir), To: homePath(filepath.Join(plan.Destination, entry.Folder)), Identity: identityKey(project.info)}
	if _, e := project.own.root.Lstat(filepath.Join(".desk-private", dialectArtifactOwnerMarker)); injectedArtifactOnlyNotExist(e) {
		if info, e := project.own.root.Lstat(".desk-private"); e == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			out.PrivateIdentity = identityKey(info)
		}
		return out, nil
	} else if e != nil {
		return out, e
	}
	private, err := openDialectArtifactOwnerFolder(ctx, project.own.root, false, "home-project-private", ".desk-private")
	if injectedArtifactOnlyNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-project-private", private.Close)) }()
	fi, err := private.Stat(".")
	if err != nil {
		return out, err
	}
	out.PrivateIdentity = identityKey(fi)
	markerRaw, markerInfo, err := readHomeMoveFile(private, dialectArtifactOwnerMarker, dialectArtifactOwnerMarkerBytes)
	if injectedArtifactOnlyNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	marker, err := readDialectArtifactOwnerMarker(markerRaw)
	if err != nil || metadata == nil {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	out.Owner = marker.Owner
	out.MarkerSHA256 = digestOf(markerRaw)
	out.MarkerIdentity = identityKey(markerInfo)
	lock, err := private.Lstat(".dialect-artifact-owner.lock")
	if err != nil || ownerOnlyFile("marker lock", lock.Mode()) != nil || ownedByUs("marker lock", lock) != nil {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	out.MarkerLockIdentity = identityKey(lock)
	locks, e := openDialectArtifactOwnerFolder(ctx, metadata, false, "home-owner-lock-snapshot", "locks", out.Owner[:2])
	if e != nil {
		return out, e
	}
	_, ownerLockInfo, e := readHomeMoveFile(locks, out.Owner[2:]+".lock", 0)
	e = errors.Join(e, closeDialectArtifactOwner(ctx, "home-owner-lock-snapshot", locks.Close))
	if e != nil {
		return out, e
	}
	out.OwnerLockIdentity = identityKey(ownerLockInfo)
	out.Namespace, err = homeMoveNamespace(ctx, private, project.dir, entry, plan, p.SourceHome == "")
	if err != nil {
		return out, err
	}
	ownerRaw, ownerInfo, folder, err := readHomeMoveRow(ctx, metadata, "owners", out.Owner, "")
	if err != nil {
		return out, err
	}
	p.Pins = append(p.Pins, homeMovePin("owners", out.Owner, "", ownerRaw, ownerInfo, folder))
	if native, e := readDialectArtifactOwnerRecord(ownerRaw); e == nil {
		if native.Owner != out.Owner || native.FolderIdentity != out.Identity || native.Work.ID != p.WorkDomain || native.Work.FolderIdentity != p.ConfigIdentity {
			return out, errDialectArtifactOwner
		}
		out.Kind = "file"
		raw, fi, folder, e := readHomeMoveRow(ctx, metadata, "owner-locations", out.Owner, "")
		if e != nil {
			return out, e
		}
		v, e := readDialectArtifactLocationRecord(raw)
		if e != nil || v.Owner != out.Owner || v.LegacySHA256 != digestOf(ownerRaw) || v.FolderIdentity != out.Identity || v.WorkDomain != p.WorkDomain {
			return out, errors.Join(errDialectArtifactOwner, e)
		}
		expected := dialectArtifactMoveLocation(project.dir, plan.Config, p.WorkDomain)
		if p.SourceHome != "" {
			expected = dialectArtifactLocation{Kind: "home", Anchor: dialectArtifactHomeAnchorID(p.RegistryOwner, p.SourceHomeIdentity), PathBytes: []byte(entry.Folder)}
		}
		a, _ := json.Marshal(v.Location)
		b, _ := json.Marshal(expected)
		if !bytes.Equal(a, b) {
			return out, errDialectArtifactOwner
		}
		p.Rows = append(p.Rows, homeMoveMutable("owner-locations", out.Owner, raw, fi, folder))
		artifacts, e := openRetainedArtifactsWithin(project.own.root, false)
		if !injectedArtifactOnlyNotExist(e) {
			if e != nil {
				return out, e
			}
			fi, e := artifacts.Stat(".")
			e = errors.Join(e, closeDialectArtifactOwner(ctx, "home-artifacts-snapshot", artifacts.Close))
			if e != nil {
				return out, e
			}
			out.ArtifactsIdentity = identityKey(fi)
		}
	} else {
		injected, e := readDialectArtifactInjectedOwnerRecord(ownerRaw)
		if e != nil || injected.Owner != out.Owner || injected.ProjectFolderIdentity != out.Identity || injected.WorkDomain != p.WorkDomain {
			return out, errors.Join(errDialectArtifactOwner, e)
		}
		out.Kind = "injected"
		out.Provider = injected.Provider
	}
	raw, fi, folder, err := readHomeMoveRow(ctx, metadata, "native", out.Owner, out.Namespace)
	if err != nil {
		return out, err
	}
	scope, err := readDialectArtifactOwnerScope(raw)
	if err != nil || scope.Kind != "native" || scope.Owner != out.Owner || scope.Namespace != out.Namespace {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	p.Pins = append(p.Pins, homeMovePin("native", out.Owner, out.Namespace, raw, fi, folder))
	return out, nil
}
func homeMoveNamespace(ctx context.Context, private *os.Root, project string, entry deskHomeEntry, plan deskHomePlan, initial bool) (namespace string, err error) {
	path := plan.ChatPath
	if !initial || entry.Kind == "desk" {
		raw, e := readDialectArtifactOwnerFile(private, dataLocationName, 16<<10)
		if e != nil {
			return "", e
		}
		var location dataLocation
		if decodeDataJSON(raw, &location) != nil || location.Version != 1 || !filepath.IsLocal(location.Path) || filepath.Clean(location.Path) != location.Path || location.Previous != "" {
			return "", errDialectArtifactOwner
		}
		path = filepath.Join(project, ".desk-private", location.Path)
	}
	root, err := openPrivateDataRoot(path, false)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-move-namespace", root.Close)) }()
	return assistantRunNamespace(root, entry.HistoryIdentity)
}

// Destination creation is the existing necessary parent creation, delayed
// until confirmation and original exclusive host custody have succeeded.
func freezeDialectArtifactHomeMove(ctx context.Context, tx *deskHomeTransaction, p *dialectArtifactHomeMovePlan, metadata *os.Root) (err error) {
	if p == nil {
		return nil
	}
	if p.SourceHome != "" {
		p.DestinationIdentity = p.SourceHomeIdentity
	} else {
		_, e := os.Lstat(tx.Destination)
		p.DestinationCreated = injectedArtifactOnlyNotExist(e)
		if e != nil && !p.DestinationCreated {
			return e
		}
		if e = makeHomeParents(tx.Destination); e != nil {
			return e
		}
		destination, e := OpenProjectRoot(tx.Destination)
		if e != nil {
			return e
		}
		p.DestinationIdentity = identityKey(destination.info)
		if destination.info.Mode().Perm()&0077 != 0 || ownedByUs("new Desk home", destination.info) != nil {
			e = errDialectArtifactOwner
		}
		err = errors.Join(e, destination.Close())
		if err != nil {
			return err
		}
		for _, write := range tx.Writes {
			if write.Path == filepath.Join(tx.Destination, deskHomePlatform, deskHomeRegistryName) {
				p.RegistryAfterSHA256 = digestOf(write.Data)
			}
		}
	}
	anchorID := dialectArtifactHomeAnchorID(p.RegistryOwner, p.DestinationIdentity)
	hasHome := false
	hasOwner := false
	for _, project := range p.Projects {
		hasOwner = hasOwner || project.Owner != ""
	}
	for _, row := range p.Rows {
		hasHome = hasHome || row.Table == "home-anchors"
	}
	if !hasHome && hasOwner {
		if p.SourceHome != "" {
			return errDialectArtifactOwner
		}
		raw, info, folder, e := readHomeMoveRow(ctx, metadata, "home-anchors", anchorID, "")
		if injectedArtifactOnlyNotExist(e) {
			p.Rows = append(p.Rows, dialectArtifactHomeMoveRow{Table: "home-anchors", ID: anchorID, BeforeKind: "absent", Before: []byte{}, BeforeSHA256: "absent", After: []byte{}, Write: -1})
		} else {
			expected, _ := json.Marshal(dialectArtifactHomeAnchorRecord{Version: 1, ID: anchorID, Kind: "home", PathBytes: []byte(tx.Destination), FolderIdentity: p.DestinationIdentity})
			if e != nil || !bytes.Equal(raw, expected) {
				return errors.Join(errDialectArtifactOwner, e)
			}
			p.Rows = append(p.Rows, homeMoveMutable("home-anchors", anchorID, raw, info, folder))
		}
	}
	// Raw writes are appended immediately before the bootstrap pointer. Their
	// paths are generated exclusively from these typed selected IDs.
	ordinary := tx.Writes
	tx.Writes = []homeWrite{}
	sort.Slice(p.Rows, func(i, j int) bool { return p.Rows[i].Table+":"+p.Rows[i].ID < p.Rows[j].Table+":"+p.Rows[j].ID })
	for i := range p.Rows {
		row := &p.Rows[i]
		switch row.Table {
		case "work-domains":
			v, e := readDialectArtifactWorkDomainRecord(row.Before)
			if e != nil {
				return e
			}
			v.PathBytes = []byte(filepath.Join(tx.Destination, deskHomePlatform))
			row.After, err = json.Marshal(v)
		case "home-anchors":
			row.After, err = json.Marshal(dialectArtifactHomeAnchorRecord{Version: 1, ID: anchorID, Kind: "home", PathBytes: []byte(tx.Destination), FolderIdentity: p.DestinationIdentity})
		case "owner-locations":
			v, e := readDialectArtifactLocationRecord(row.Before)
			if e != nil {
				return e
			}
			for _, project := range p.Projects {
				if project.Owner == row.ID {
					v.Location = dialectArtifactLocation{Kind: "home", Anchor: anchorID, PathBytes: []byte(project.Folder)}
				}
			}
			row.After, err = marshalDialectArtifactLocation(v)
		default:
			return errDialectArtifactOwner
		}
		if err != nil {
			return err
		}
		row.AfterSHA256 = digestOf(row.After)
		row.Write = len(tx.Writes)
		tx.Writes = append(tx.Writes, homeWrite{Path: homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), row.Table, row.ID), Before: row.BeforeSHA256, Data: bytes.Clone(row.After)})
	}
	tx.Writes = append(tx.Writes, ordinary...)
	tx.Version = 2
	tx.Retained = p
	if !p.valid(tx) {
		return errDialectArtifactOwner
	}
	_, err = marshalHomeTransaction(*tx)
	return err
}

func homeMoveProviderIDs(p *dialectArtifactHomeMovePlan) []string {
	ids := []string{}
	if p != nil {
		for _, project := range p.Projects {
			if project.Provider != "" {
				ids = append(ids, project.Provider)
			}
		}
	}
	return ids
}
func homeMoveOwnerProjects(p *dialectArtifactHomeMovePlan) []dialectArtifactHomeMoveProject {
	out := []dialectArtifactHomeMoveProject{}
	if p != nil {
		for _, project := range p.Projects {
			if project.Owner != "" {
				out = append(out, project)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Owner < out[j].Owner })
	return out
}

// Kept local so no persisted locator or arbitrary caller path can select an
// owner table. Every caller already owns the lexical home transaction hold.
func openHomeMoveMetadata(ctx context.Context, config *os.Root) (secrets, metadata *os.Root, err error) {
	secrets, err = openDialectArtifactOwnerFolder(ctx, config, false, "home-move-secrets", secretsDirName)
	if err != nil {
		return nil, nil, err
	}
	metadata, err = openDialectArtifactOwnerFolder(ctx, secrets, false, "home-move-metadata", dialectArtifactOwnersFolder)
	if err != nil {
		err = errors.Join(err, secrets.Close())
		secrets = nil
	}
	return
}
