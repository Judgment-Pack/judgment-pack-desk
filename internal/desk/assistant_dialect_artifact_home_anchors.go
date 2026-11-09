package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const dialectArtifactHomeAnchorRecordBytes = 5639 // base64(4096)=5464 + fixed shape 175

// A private per-call observation seam for the inspection/open race vectors.
// It grants no custody and cannot replace any pin or validation result.
type dialectArtifactHomeEdgeInspectionKey struct{}

type dialectArtifactHomeAnchorRecord struct {
	Version        int    `json:"version"`
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	PathBytes      []byte `json:"pathBytes"`
	FolderIdentity string `json:"folderIdentity"`
}

func dialectArtifactHomeAnchorID(owner, identity string) string {
	raw, _ := json.Marshal([]string{"dialect-artifact-home-v1", owner, identity})
	return digestOf(raw)
}

func (v dialectArtifactHomeAnchorRecord) valid() bool {
	return v.Version == 1 && assistantRunDigest(v.ID) && v.Kind == "home" &&
		dialectArtifactOwnerPath(v.PathBytes) && dialectArtifactOwnerFileIdentity(v.FolderIdentity)
}

func readDialectArtifactHomeAnchorRecord(raw []byte) (dialectArtifactHomeAnchorRecord, error) {
	var v dialectArtifactHomeAnchorRecord
	err := readDialectArtifactOwnerJSON(raw, dialectArtifactHomeAnchorRecordBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

// Private owned custody, never a serialized token or caller-supplied proof.
// The borrowed original home lease is only inspected, never relocked/closed.
type dialectArtifactHomeHold struct {
	s             *Server
	owner         *Server
	ctx           context.Context
	root          *os.Root
	shared        *os.File
	registry      *os.File
	leaseInfo     os.FileInfo
	registryInfo  os.FileInfo
	homeInfo      os.FileInfo
	workInfo      os.FileInfo
	registryRaw   []byte
	registryOwner string
	entry         deskHomeEntry
	record        dialectArtifactHomeAnchorRecord
}

func (h *dialectArtifactHomeHold) close() error {
	var err error
	if h.registry != nil {
		err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-registry", h.registry.Close))
		h.registry = nil
	}
	if h.root != nil {
		err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-root", h.root.Close))
		h.root = nil
	}
	if h.shared != nil {
		err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-hold", h.shared.Close))
		h.shared = nil
	}
	return err
}

func sameDialectArtifactHomeBinding(a, b deskHomeEntry) bool {
	// Display names may change without changing stable identity.
	return a.ID == b.ID && a.Folder == b.Folder && a.Kind == b.Kind && a.HistoryIdentity == b.HistoryIdentity &&
		a.StorageOwner == b.StorageOwner && a.RunnerWorkspace == b.RunnerWorkspace
}

// Called under the actual owner's desksMu and selected location.use, before
// work custody. A native _platform installation can never fall back to work.
func (s *Server) holdDialectArtifactHome(ctx context.Context) (_ *dialectArtifactHomeHold, err error) {
	owner := s.deskOwner()
	configured := filepath.Base(s.configDir) == deskHomePlatform || s.home != nil || owner.home != nil
	if !configured {
		return nil, nil
	}
	if s.home == nil || owner.home == nil || owner.homeLease == nil || owner.assistant == nil || !owner.assistant.usable() ||
		owner.configDir != s.configDir || filepath.Base(s.configDir) != deskHomePlatform ||
		s.home.Path != filepath.Dir(s.configDir) || owner.home.Path != s.home.Path {
		return nil, errDialectArtifactOwner
	}
	h := &dialectArtifactHomeHold{s: s, owner: owner, ctx: ctx}
	defer func() {
		if err != nil {
			err = errors.Join(err, h.close())
		}
	}()
	h.leaseInfo, err = owner.homeLease.Stat()
	if err != nil {
		return nil, err
	}
	entry, err := owner.assistant.root.Lstat(".home-use.lock")
	if err != nil || !os.SameFile(entry, h.leaseInfo) || ownerOnlyFile("home lease", entry.Mode()) != nil || ownedByUs("home lease", entry) != nil {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.shared, err = owner.assistant.root.OpenFile(".home-use.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	sharedInfo, err := h.shared.Stat()
	if err != nil || !os.SameFile(sharedInfo, h.leaseInfo) || ownerOnlyFile("home lease", sharedInfo.Mode()) != nil || ownedByUs("home lease", sharedInfo) != nil {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	if err = lockPrivateData(h.shared, false); err != nil {
		return nil, err
	}
	h.workInfo, err = s.assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	parentWork, err := owner.assistant.root.Stat(".")
	if err != nil || !os.SameFile(h.workInfo, parentWork) {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.registry, err = openDialectArtifactOwnerFile(s.assistant.root, deskHomeRegistryName)
	if err != nil {
		return nil, err
	}
	h.registryInfo, err = h.registry.Stat()
	if err != nil {
		return nil, err
	}
	h.registryRaw, err = readBounded(h.registry, 1<<20)
	if err != nil {
		return nil, err
	}
	registry, decodeErr := decodeHomeRegistry(h.registryRaw)
	if decodeErr != nil ||
		registry.Owner != s.home.Registry.Owner || registry.Owner != owner.home.Registry.Owner {
		return nil, errDialectArtifactOwner
	}
	h.registryOwner = registry.Owner
	fresh := (&deskHome{Path: s.home.Path, Registry: registry}).entry(s.projectDir)
	cached := s.homeEntry()
	if fresh == nil || cached == nil || !sameDialectArtifactHomeBinding(*fresh, *cached) ||
		(fresh.Kind == "desk" && fresh.ID != s.cfg.deskID) || (fresh.Kind == "project" && s.cfg.deskID != "") {
		return nil, errDialectArtifactOwner
	}
	h.entry = *fresh
	h.homeInfo, err = os.Lstat(s.home.Path)
	if err != nil || !h.homeInfo.IsDir() || h.homeInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.root, err = os.OpenRoot(s.home.Path)
	if err != nil {
		return nil, err
	}
	held, err := h.root.Stat(".")
	if err != nil || !os.SameFile(h.homeInfo, held) {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	h.record = dialectArtifactHomeAnchorRecord{Version: 1, Kind: "home", PathBytes: []byte(s.home.Path), FolderIdentity: identityKey(held)}
	h.record.ID = dialectArtifactHomeAnchorID(registry.Owner, h.record.FolderIdentity)
	if !h.record.valid() {
		return nil, errDialectArtifactOwner
	}
	if err = h.check(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *dialectArtifactHomeHold) namespace(root *os.Root, expected string) error {
	namespace, err := assistantRunNamespace(root, h.entry.HistoryIdentity)
	if err != nil || namespace != expected {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return nil
}

// Leaf pin checker: no lock acquisition and no openChatData/recursive enrollment.
func (h *dialectArtifactHomeHold) check() (err error) {
	if err = h.ctx.Err(); err != nil {
		return err
	}
	original, err := h.owner.homeLease.Stat()
	if err != nil || !os.SameFile(original, h.leaseInfo) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	entry, err := h.owner.assistant.root.Lstat(".home-use.lock")
	if err != nil || !os.SameFile(entry, h.leaseInfo) || ownerOnlyFile("home lease", entry.Mode()) != nil || ownedByUs("home lease", entry) != nil {
		return errors.Join(errDialectArtifactOwner, err)
	}
	shared, err := h.shared.Stat()
	if err != nil || !os.SameFile(shared, h.leaseInfo) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if !sameDirectory(h.s.home.Path, h.homeInfo) || !sameDirectory(h.s.configDir, h.workInfo) || !sameDirectory(h.s.projectDir, h.s.project.info) {
		return errDialectArtifactOwner
	}
	currentHome, err := os.Lstat(h.s.home.Path)
	if err != nil || !currentHome.IsDir() || currentHome.Mode()&os.ModeSymlink != 0 || !os.SameFile(currentHome, h.homeInfo) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	for _, selected := range []struct {
		name string
		info os.FileInfo
	}{{deskHomePlatform, h.workInfo}, {h.entry.Folder, h.s.project.info}} {
		info, lookErr := h.root.Lstat(selected.name)
		if lookErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, selected.info) {
			return errors.Join(errDialectArtifactOwner, lookErr)
		}
		if inspected, ok := h.ctx.Value(dialectArtifactHomeEdgeInspectionKey{}).(func(string) error); ok {
			if err = inspected(selected.name); err != nil {
				return err
			}
		}
		root, openErr := h.root.OpenRoot(selected.name)
		if openErr != nil {
			return openErr
		}
		held, statErr := root.Stat(".")
		again, againErr := h.root.Lstat(selected.name)
		closeErr := closeDialectArtifactOwner(h.ctx, "home-edge", root.Close)
		if statErr != nil || againErr != nil || closeErr != nil || !os.SameFile(info, held) || !os.SameFile(held, selected.info) ||
			!again.IsDir() || again.Mode()&os.ModeSymlink != 0 || !os.SameFile(held, again) {
			return errors.Join(errDialectArtifactOwner, statErr, againErr, closeErr)
		}
	}
	file, err := openDialectArtifactOwnerFile(h.s.assistant.root, deskHomeRegistryName)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-registry-check", file.Close)) }()
	info, err := file.Stat()
	if err != nil || !os.SameFile(info, h.registryInfo) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	raw, err := readBounded(file, 1<<20)
	if err != nil || !bytes.Equal(raw, h.registryRaw) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	// Formula is checked from fresh selected registry continuity and held inode,
	// not from a row ID or a copied cached registry.
	if h.record.ID != dialectArtifactHomeAnchorID(h.registryOwner, identityKey(h.homeInfo)) {
		return errDialectArtifactOwner
	}
	return nil
}

func syncDialectArtifactHomeRoot(ctx context.Context, root *os.Root, label string) (err error) {
	owned, err := root.OpenRoot(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, label, owned.Close)) }()
	if err = syncDialectArtifactOwnerDirectory(owned); err != nil {
		return err
	}
	return dialectArtifactOwnerStep(ctx, label+".sync")
}

func (h *dialectArtifactHomeHold) publish(metadata *os.Root, check func() error) (raw []byte, err error) {
	defer func() {
		if err != nil {
			raw = nil
		}
	}()
	if err = check(); err != nil {
		return nil, err
	}
	if h.registry == nil {
		return nil, errDialectArtifactOwner
	}
	if err = h.registry.Sync(); err != nil {
		return nil, err
	}
	if err = dialectArtifactOwnerStep(h.ctx, "home-registry.sync"); err != nil {
		return nil, err
	}
	file := h.registry
	h.registry = nil
	if err = closeDialectArtifactOwner(h.ctx, "home-registry", file.Close); err != nil {
		return nil, err
	}
	if err = syncDialectArtifactHomeRoot(h.ctx, h.s.assistant.root, "home-config"); err != nil {
		return nil, err
	}
	if err = syncDialectArtifactHomeRoot(h.ctx, h.root, "home-parent"); err != nil {
		return nil, err
	}
	if err = check(); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(h.record)
	if err != nil || len(raw) > dialectArtifactHomeAnchorRecordBytes {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	rows, err := openDialectArtifactOwnerFolder(h.ctx, metadata, true, "home-anchor-folder", "home-anchors", h.record.ID[:2])
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(h.ctx, "home-anchor-folder", rows.Close)) }()
	if err = claimDialectArtifactOwner(h.ctx, rows, h.record.ID[2:]+".json", raw, "home-anchor", check); err != nil {
		return nil, err
	}
	return raw, nil
}

func readbackDialectArtifactHomeAnchor(ctx context.Context, metadata *os.Root, expected []byte) (err error) {
	v, err := readDialectArtifactHomeAnchorRecord(expected)
	if err != nil {
		return err
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "home-anchor-folder", "home-anchors", v.ID[:2])
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "home-anchor-readback", rows.Close)) }()
	raw, err := readDialectArtifactOwnerFile(rows, v.ID[2:]+".json", dialectArtifactHomeAnchorRecordBytes)
	if err != nil || !bytes.Equal(raw, expected) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if _, err = readDialectArtifactHomeAnchorRecord(raw); err != nil {
		return err
	}
	return dialectArtifactOwnerStep(ctx, "home-anchor.selected-readback")
}
