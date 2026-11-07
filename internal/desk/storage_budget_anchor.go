package desk

// This is an inactive B2.1a foundation. No constructor below is called by New.
// It requires the original existing private config and existing home-use lease.
// It neither selects a workspace fallback nor initializes partial installations.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageAnchorName    = "storage-budget"
	storageClaimName     = "storage-installation.claim.json"
	storageRecordName    = "storage-installation.json"
	storageBootstrapName = ".storage-bootstrap.lock"
	storageUseName       = ".storage-use.lock"
	storageIntentName    = "storage-host-intents"
	storageReadyName     = "storage-installation.ready"
	storageIntentBytes   = 2 << 20
	storageHostBudget    = 4 << 20
	storageRecordLimit   = 4096
)

var (
	errStorageCustody      = errors.New("storage original installation custody unavailable")
	errStorageBootstrap    = errors.New("storage original bootstrap incomplete; original-owner recovery required")
	errStoragePrerequisite = errors.New("storage bootstrap requires existing private config and original home exclusion")
)

// These are original directory witnesses, not portable identities. Copying the
// control bytes to a different inode never establishes original installation.
type storageDirectoryIdentity struct{ Device, Inode uint64 }
type storageInstallationRecord struct {
	Version        uint32
	Kind           string
	Installation   storagebudget.ID
	Nonce          storagebudget.ID
	Config, Anchor storageDirectoryIdentity
	HomeOwner      string
}

type storageNamedRoot struct {
	parent   *os.Root
	name     string
	root     *os.Root
	identity os.FileInfo
	private  bool
}
type storageNamedFile struct {
	root      *os.Root
	name      string
	file      *os.File
	identity  os.FileInfo
	size      int64
	immutable []byte // bounded public metadata only; never credential content
}
type storageOriginal struct {
	roots  []storageNamedRoot // absolute configured ancestry, then config
	files  []storageNamedFile
	config *os.Root
	lease  *os.File
}

func storagePrivate(info os.FileInfo, directory bool) bool {
	if !custodyChecked || info == nil {
		return false
	}
	uid, ok := ownerOf(info)
	if !ok || uid != effectiveUser() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode() & ^(os.ModeDir|os.FileMode(0777)) == 0 && info.Mode().Perm()&0700 == 0700
	}
	links, ok := linkCount(info)
	return ok && links == 1 && info.Mode().IsRegular() && info.Mode() & ^os.FileMode(0777) == 0 && info.Mode().Perm()&0600 == 0600
}
func (n storageNamedRoot) current() error {
	before, e := n.parent.Lstat(n.name)
	if e != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, n.identity) {
		return errStorageCustody
	}
	held, e := n.root.Stat(".")
	if e != nil || !os.SameFile(before, held) || n.private && !storagePrivate(held, true) {
		return errStorageCustody
	}
	after, e := n.parent.Lstat(n.name)
	if e != nil || !os.SameFile(after, held) || after.Mode()&os.ModeSymlink != 0 || n.private && !storagePrivate(after, true) {
		return errStorageCustody
	}
	return nil
}
func storagePinChild(parent *os.Root, name string, private bool) (storageNamedRoot, error) {
	var n storageNamedRoot
	before, e := parent.Lstat(name)
	if e != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || private && !storagePrivate(before, true) {
		return n, errStorageCustody
	}
	child, e := parent.OpenRoot(name)
	if e != nil {
		return n, errStorageCustody
	}
	n = storageNamedRoot{parent, name, child, before, private}
	if e = n.current(); e != nil {
		_ = child.Close()
		return storageNamedRoot{}, e
	}
	return n, nil
}
func storageOpenFile(root *os.Root, name string, size int64) (storageNamedFile, error) {
	var n storageNamedFile
	before, e := root.Lstat(name)
	if e != nil || !storagePrivate(before, false) || before.Size() != size {
		return n, errStorageCustody
	}
	f, e := root.OpenFile(name, os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if e != nil {
		return n, errStorageCustody
	}
	n = storageNamedFile{root: root, name: name, file: f, identity: before, size: size}
	if e = n.current(); e != nil {
		_ = f.Close()
		return storageNamedFile{}, e
	}
	return n, nil
}
func (n storageNamedFile) current() error {
	before, e := n.root.Lstat(n.name)
	if e != nil || !storagePrivate(before, false) || before.Size() != n.size || !os.SameFile(before, n.identity) {
		return errStorageCustody
	}
	held, e := n.file.Stat()
	if e != nil || !storagePrivate(held, false) || held.Size() != n.size || !os.SameFile(before, held) {
		return errStorageCustody
	}
	if n.immutable != nil {
		b := make([]byte, len(n.immutable))
		if _, e = n.file.ReadAt(b, 0); e != nil || !bytes.Equal(b, n.immutable) {
			return errStorageCustody
		}
	}
	after, e := n.root.Lstat(n.name)
	if e != nil || !storagePrivate(after, false) || after.Size() != n.size || !os.SameFile(after, held) {
		return errStorageCustody
	}
	return nil
}
func (o *storageOriginal) current() error {
	for _, n := range o.roots {
		if e := n.current(); e != nil {
			return e
		}
	}
	for _, n := range o.files {
		if e := n.current(); e != nil {
			return e
		}
	}
	return nil
}
func (o *storageOriginal) close() (err error) {
	for i := len(o.files) - 1; i >= 0; i-- {
		err = errors.Join(err, o.files[i].file.Close())
	}
	// The first root owns the filesystem root; every subsequent child owns one
	// independent directory handle. No borrowed parent handle is closed twice.
	for i := len(o.roots) - 1; i >= 0; i-- {
		err = errors.Join(err, o.roots[i].root.Close())
	}
	if len(o.roots) > 0 {
		err = errors.Join(err, o.roots[0].parent.Close())
	}
	return err
}
func storagePinOriginal(config string) (*storageOriginal, error) {
	if !custodyChecked || !filepath.IsAbs(config) || filepath.Clean(config) != config || len(config) > 4096 {
		return nil, errStoragePrerequisite
	}
	parts := strings.Split(strings.TrimPrefix(config, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) < 2 || len(parts) > 64 {
		return nil, errStoragePrerequisite
	}
	base, e := os.OpenRoot(string(filepath.Separator))
	if e != nil {
		return nil, errStorageCustody
	}
	o := &storageOriginal{}
	parent := base
	for i, name := range parts {
		// Both the original existing config and its immediate parent are private.
		n, e := storagePinChild(parent, name, i >= len(parts)-2)
		if e != nil {
			if len(o.roots) == 0 {
				_ = base.Close()
			} else {
				_ = o.close()
			}
			return nil, e
		}
		o.roots = append(o.roots, n)
		parent = n.root
	}
	o.config = parent
	lease, e := storageOpenFile(parent, ".home-use.lock", 0)
	if e != nil {
		_ = o.close()
		return nil, errStoragePrerequisite
	}
	o.files = append(o.files, lease)
	o.lease = lease.file
	return o, nil
}

func storageRecordBytes(r storageInstallationRecord) ([]byte, error) {
	if r.Version != 1 || r.Kind != "claim" && r.Kind != "installation" || r.Installation == (storagebudget.ID{}) || r.Nonce == (storagebudget.ID{}) || r.Config == (storageDirectoryIdentity{}) || len(r.HomeOwner) == 0 || len(r.HomeOwner) > 256 || strings.ContainsAny(r.HomeOwner, "\x00\r\n") || r.Kind == "claim" && r.Anchor != (storageDirectoryIdentity{}) || r.Kind == "installation" && r.Anchor == (storageDirectoryIdentity{}) {
		return nil, errStorageCustody
	}
	b, e := json.Marshal(r)
	if e != nil || len(b)+1 > storageRecordLimit {
		return nil, errStorageCustody
	}
	return append(b, '\n'), nil
}
func storageReadRecord(root *os.Root, name string) (storageInstallationRecord, storageNamedFile, []byte, error) {
	var r storageInstallationRecord
	info, e := root.Lstat(name)
	if e != nil || info.Size() <= 0 || info.Size() > storageRecordLimit {
		return r, storageNamedFile{}, nil, errStorageBootstrap
	}
	n, e := storageOpenFile(root, name, info.Size())
	if e != nil {
		return r, n, nil, e
	}
	b := make([]byte, n.size)
	_, e = n.file.ReadAt(b, 0)
	if e == nil {
		e = json.Unmarshal(b, &r)
	}
	canonical, ce := storageRecordBytes(r)
	if e != nil || ce != nil || !bytes.Equal(b, canonical) || n.current() != nil {
		_ = n.file.Close()
		return storageInstallationRecord{}, storageNamedFile{}, nil, errStorageBootstrap
	}
	n.immutable = append([]byte(nil), b...)
	return r, n, b, nil
}

// The cell, each ref and each write-lifetime use are address sealed. A copied
// wrapper cannot decrement custody or close a descriptor whose number was reused.
type storageInstallation struct {
	self               *storageInstallation
	mu                 sync.Mutex
	refs               uint64
	closing, closed    bool
	original           *storageOriginal
	anchor             *os.Root
	record             storageInstallationRecord
	ownerRecord        *storageOwnerRecordV2
	ownerClaim         storageOwnerClaimV2
	ownerCatalog       storageOwnerCatalogV2
	enrollmentEndpoint *storageEnrollmentPeakEvidence
	startup            *storageBudgetConstruction
	recordBytes        []byte
	binding            storagebudget.Digest
	q                  *storagebudget.Coordinator
	closeErr           error
}
type storageInstallationRef struct {
	self            *storageInstallationRef
	cell            *storageInstallation
	owner, released bool
}
type storageInstallationUse struct {
	self     *storageInstallationUse
	cell     *storageInstallation
	gate     *os.File
	released bool
}

func storageOwner(o *storageOriginal, anchor *os.Root, r storageInstallationRecord, b []byte, binding storagebudget.Digest, q *storagebudget.Coordinator) *storageInstallationRef {
	c := &storageInstallation{refs: 1, original: o, anchor: anchor, record: r, recordBytes: append([]byte(nil), b...), binding: binding, q: q}
	c.self = c
	ref := &storageInstallationRef{cell: c, owner: true}
	ref.self = ref
	return ref
}
func (r *storageInstallationRef) valid() bool {
	return r != nil && r.self == r && r.cell != nil && r.cell.self == r.cell
}
func (r *storageInstallationRef) borrow() (*storageInstallationRef, error) {
	if !r.valid() {
		return nil, errStorageCustody
	}
	c := r.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.released || c.closing || c.closed {
		return nil, os.ErrClosed
	}
	if c.refs == ^uint64(0) {
		return nil, storagebudget.ErrBounds
	}
	c.refs++
	out := &storageInstallationRef{cell: c}
	out.self = out
	return out, nil
}
func (c *storageInstallation) dropLocked() error {
	c.refs--
	if c.refs != 0 {
		return nil
	}
	c.closed = true
	// Q closes its independently opened description before borrowed witnesses.
	c.closeErr = errors.Join(c.q.Close(), c.original.close())
	return c.closeErr
}
func (r *storageInstallationRef) Close() error {
	if !r.valid() {
		return errStorageCustody
	}
	c := r.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.released {
		return c.closeErr
	}
	r.released = true
	if r.owner {
		c.closing = true
	}
	return c.dropLocked()
}
func (c *storageInstallation) current() error {
	if c.ownerRecord != nil {
		return c.currentOwnerV2()
	}
	if e := c.original.current(); e != nil {
		return e
	}
	id, e := storageDirectoryID(c.original.config)
	if e != nil || id != c.record.Config {
		return errStorageCustody
	}
	id, e = storageDirectoryID(c.anchor)
	if e != nil || id != c.record.Anchor {
		return errStorageCustody
	}
	// Re-read bounded public control bytes, never credentials or their digests.
	for _, n := range c.original.files {
		if n.name == storageRecordName || n.name == storageClaimName || n.name == storageReadyName {
			b := make([]byte, n.size)
			if _, e = n.file.ReadAt(b, 0); e != nil {
				return errStorageCustody
			}
			switch n.name {
			case storageRecordName:
				if !bytes.Equal(b, c.recordBytes) {
					return errStorageCustody
				}
			case storageClaimName:
				claim := c.record
				claim.Kind = "claim"
				claim.Anchor = storageDirectoryIdentity{}
				want, _ := storageRecordBytes(claim)
				if !bytes.Equal(b, want) {
					return errStorageCustody
				}
			case storageReadyName:
				if !bytes.Equal(b, c.binding[:]) {
					return errStorageCustody
				}
			}
		}
	}
	return c.original.current()
}
func (r *storageInstallationRef) acquire() (*storageInstallationUse, error) {
	if !r.valid() {
		return nil, errStorageCustody
	}
	c := r.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.released || c.closing || c.closed {
		return nil, os.ErrClosed
	}
	if c.refs == ^uint64(0) {
		return nil, storagebudget.ErrBounds
	}
	if e := c.current(); e != nil {
		return nil, e
	}
	n, e := storageOpenFile(c.original.config, storageUseName, 0)
	if e != nil {
		return nil, e
	}
	return c.acquireUseGateLocked(n.file)
}

// useGateCurrent binds a fresh description to the retained original witness,
// even if an intervening replacement name was restored before currentness ran.
// It never promotes the fresh description into original authority.
func (c *storageInstallation) useGateCurrent(gate *os.File) error {
	if gate == nil {
		return errStorageCustody
	}
	for _, original := range c.original.files {
		if original.root != c.original.config || original.name != storageUseName {
			continue
		}
		if e := original.current(); e != nil {
			return e
		}
		witness, e := original.file.Stat()
		if e != nil {
			return errStorageCustody
		}
		held, e := gate.Stat()
		if e != nil || !storagePrivate(held, false) || held.Size() != 0 || !os.SameFile(held, witness) {
			return errStorageCustody
		}
		return original.current()
	}
	return errStorageCustody
}

// Caller holds c.mu and transfers this fresh description. On refusal it closes
// only that description; a granted use retains it through the write lifetime.
func (c *storageInstallation) acquireUseGateLocked(gate *os.File) (*storageInstallationUse, error) {
	refuse := func(e error) (*storageInstallationUse, error) {
		if gate != nil {
			_ = gate.Close()
		}
		return nil, e
	}
	if e := c.useGateCurrent(gate); e != nil {
		return refuse(e)
	}
	if e := lockPrivateData(gate, false); e != nil {
		return refuse(storagebudget.ErrBusy)
	}
	if e := c.current(); e != nil {
		return refuse(e)
	}
	if e := c.useGateCurrent(gate); e != nil {
		return refuse(e)
	}
	c.refs++
	u := &storageInstallationUse{cell: c, gate: gate}
	u.self = u
	return u, nil
}

// originalCurrentLocked is available only through the actual retained use,
// under cell.mu. It does not change fresh c.current/acquire, admission phases or
// mutation authority. Construction Closing alone cannot revoke physical custody
// before its original borrowers drain; a completed Close still refuses.
func (u *storageInstallationUse) originalCurrentLocked() error {
	c := u.cell
	if u.self != u || u.released || u.gate == nil || c == nil || c.self != c || c.closed {
		return os.ErrClosed
	}
	if c.ownerRecord == nil {
		return c.current()
	}
	startup := c.startup
	if startup == nil || startup.self != startup || startup.ref == nil || !startup.ref.valid() || startup.ref.cell != c || startup.original != c.original {
		return errStorageCustody
	}
	startup.mu.Lock()
	closed := startup.closed
	startup.mu.Unlock()
	if closed {
		return os.ErrClosed
	}
	if err := startup.originPhysicalCurrent(context.Background()); err != nil {
		return err
	}
	return c.ownerV2PhysicalCurrent()
}
func (u *storageInstallationUse) Current() error {
	if u == nil || u.self != u || u.cell == nil || u.cell.self != u.cell {
		return errStorageCustody
	}
	c := u.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if u.released || c.closed {
		return os.ErrClosed
	}
	if e := u.originalCurrentLocked(); e != nil {
		return e
	}
	if e := c.useGateCurrent(u.gate); e != nil {
		return e
	}
	return u.originalCurrentLocked()
}
func (u *storageInstallationUse) Close() error {
	if u == nil || u.self != u || u.cell == nil || u.cell.self != u.cell {
		return errStorageCustody
	}
	c := u.cell
	c.mu.Lock()
	defer c.mu.Unlock()
	if u.released {
		return c.closeErr
	}
	u.released = true
	return errors.Join(u.gate.Close(), c.dropLocked())
}

// openStorageInstallation is custody-only. Missing, partial or foreign state is
// retained verbatim; no call to InitializeHeld, mkdir, create, truncate or repair.
func openStorageInstallation(ctx context.Context, config, homeOwner string) (_ *storageInstallationRef, err error) {
	o, e := storagePinOriginal(config)
	if e != nil {
		return nil, e
	}
	adopted := false
	defer func() {
		if !adopted {
			err = errors.Join(err, o.close())
		}
	}()
	bootstrap, e := storageOpenFile(o.config, storageBootstrapName, 0)
	if e != nil {
		return nil, errStorageBootstrap
	}
	o.files = append(o.files, bootstrap)
	if e = lockPrivateData(bootstrap.file, true); e != nil {
		return nil, storagebudget.ErrBusy
	}
	ref, e := storageOpenComplete(ctx, o, homeOwner)
	if e != nil {
		return nil, e
	}
	if e = storageUnlock(bootstrap.file); e != nil {
		_ = ref.cell.q.Close()
		return nil, e
	}
	adopted = true
	return ref, nil
}
func storageOpenComplete(ctx context.Context, o *storageOriginal, homeOwner string) (*storageInstallationRef, error) {
	claim, cn, _, e := storageReadRecord(o.config, storageClaimName)
	if e != nil {
		return nil, e
	}
	o.files = append(o.files, cn)
	record, rn, b, e := storageReadRecord(o.config, storageRecordName)
	if e != nil {
		return nil, e
	}
	o.files = append(o.files, rn)
	want := record
	want.Kind = "claim"
	want.Anchor = storageDirectoryIdentity{}
	if claim != want || record.Kind != "installation" || record.HomeOwner != homeOwner {
		return nil, errStorageCustody
	}
	anchor, e := storagePinChild(o.config, storageAnchorName, true)
	if e != nil {
		return nil, errStorageBootstrap
	}
	o.roots = append(o.roots, anchor)
	binding := sha256.Sum256(append([]byte("desk-storage-installation-v1\x00"), b...))
	for _, row := range []struct {
		root *os.Root
		name string
		size int64
	}{{o.config, storageUseName, 0}, {o.config, storageIntentName, storageIntentBytes}, {o.config, storageReadyName, 32}, {anchor.root, "lock", 0}} {
		n, e := storageOpenFile(row.root, row.name, row.size)
		if e != nil {
			return nil, errStorageBootstrap
		}
		o.files = append(o.files, n)
	}
	lock := o.files[len(o.files)-1].file
	// Pin every fixed control inode as well as Q's stable lock. Q validates
	// bounded page contents; Desk additionally refuses name replacement.
	for _, row := range []struct {
		name string
		size int64
	}{{"checkpoint.a", storagebudget.CheckpointBytes}, {"checkpoint.b", storagebudget.CheckpointBytes}, {"tickets.a", storagebudget.Slots * storagebudget.TicketBytes}, {"tickets.b", storagebudget.Slots * storagebudget.TicketBytes}, {"manifests", storagebudget.Slots * storagebudget.ManifestBytes}} {
		n, e := storageOpenFile(anchor.root, row.name, row.size)
		if e != nil {
			return nil, errStorageBootstrap
		}
		o.files = append(o.files, n)
	}
	dir, e := anchor.root.Open(".")
	if e != nil {
		return nil, errStorageBootstrap
	}
	names, re := dir.ReadDir(7)
	ce := dir.Close()
	if re != nil || ce != nil || len(names) != 6 {
		return nil, errStorageBootstrap
	}
	q, e := storagebudget.OpenHeld(storagebudget.HeldAnchor{Root: anchor.root, LockWitness: lock, Installation: record.Installation, Binding: binding, AdvisoryLimits: storageLimitsAdvisory})
	if e != nil {
		return nil, e
	}
	ref := storageOwner(o, anchor.root, record, b, binding, q)
	if e = ref.cell.current(); e != nil {
		_ = q.Close()
		return nil, e
	}
	if _, e = q.ConfigurationSnapshot(ctx); e != nil {
		_ = q.Close()
		return nil, errStorageBootstrap
	}
	return ref, nil
}

type storageBootstrapOptions struct {
	Policy                        storagebudget.Policy
	HeadroomBytes, HeadroomInodes uint64
}

// A package-private deterministic test seam. It never executes a caller callback.
type storageBootstrapFault struct{ Name, Phase string }

func (f *storageBootstrapFault) fail(name, phase string) error {
	if f != nil && f.Name == name && f.Phase == phase {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func storageBootstrapPreflight(ctx context.Context, o *storageOriginal, opt storageBootstrapOptions) error {
	p := opt.Policy
	envelope := storagebudget.Vector{Bytes: storagebudget.MetadataBudget + storageHostBudget, Files: 32, Directories: 5}
	validVector := func(v storagebudget.Vector) bool {
		return v.Bytes > 0 && v.Bytes <= 1<<60 && v.Files > 0 && v.Files <= 1<<40 && v.Directories > 0 && v.Directories <= 1<<40
	}
	fits := func(v storagebudget.Vector) bool {
		return envelope.Bytes <= v.Bytes && envelope.Files <= v.Files && envelope.Directories <= v.Directories
	}
	if !validVector(p.Installation.Total) || !validVector(p.Workspace.Total) || !fits(p.Installation.Total) || p.ObservationMaxAge <= 0 || p.ObservationMaxAge > time.Hour {
		return storagebudget.ErrBounds
	}
	for i := 0; i < storagebudget.Categories; i++ {
		if !validVector(p.Installation.Category[i]) || !validVector(p.Workspace.Category[i]) {
			return storagebudget.ErrBounds
		}
	}
	if !fits(p.Installation.Category[storagebudget.Control]) || opt.HeadroomBytes > 1<<60-envelope.Bytes || opt.HeadroomInodes > 1<<40-37 {
		return storagebudget.ErrBounds
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	obs, e := storagebudget.ObserveHeld(o.config, storagebudget.Filesystem{ID: storagebudget.ID{1}, Generation: 1})
	if e != nil {
		return e
	}
	return storageBootstrapCapacity(opt, obs)
}
func storageBootstrapCapacity(opt storageBootstrapOptions, obs storagebudget.Observation) error {
	if !obs.Known || time.Since(obs.At) < 0 || time.Since(obs.At) > opt.Policy.ObservationMaxAge {
		return storagebudget.ErrUnknown
	}
	if opt.HeadroomBytes > 1<<60-(storagebudget.MetadataBudget+storageHostBudget) || opt.HeadroomInodes > 1<<40-37 {
		return storagebudget.ErrBounds
	}
	if obs.AvailableBytes < storagebudget.MetadataBudget+storageHostBudget+opt.HeadroomBytes || obs.AvailableInodes < 37+opt.HeadroomInodes {
		return storagebudget.ErrHeadroom
	}
	return nil
}
func storageSyncRoot(root *os.Root, name string, fault *storageBootstrapFault) error {
	d, e := root.Open(".")
	if e != nil {
		return errStorageBootstrap
	}
	e = fault.fail(name, "directory-sync")
	if e == nil {
		e = d.Sync()
	}
	ce := d.Close()
	fe := fault.fail(name, "directory-close")
	if e != nil || ce != nil || fe != nil {
		return errStorageBootstrap
	}
	return nil
}
func storageCreateControl(root *os.Root, name string, data []byte, size int64, fault *storageBootstrapFault) error {
	// This helper can create only the protocol's finite declared leaves.
	valid := false
	switch name {
	case storageClaimName, storageRecordName:
		valid = size > 0 && size <= storageRecordLimit && int64(len(data)) == size
	case storageBootstrapName, storageUseName, "lock":
		valid = size == 0 && len(data) == 0
	case storageReadyName:
		valid = size == 32 && len(data) == 32
	case storageIntentName:
		valid = size == storageIntentBytes && len(data) == 0
	}
	if !valid {
		return storagebudget.ErrBounds
	}
	if size < 0 || size > storageIntentBytes || int64(len(data)) > size {
		return storagebudget.ErrBounds
	}
	if e := fault.fail(name, "create"); e != nil {
		return errStorageBootstrap
	}
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
	if e != nil {
		return errStorageBootstrap
	}
	if e = fault.fail(name, "write"); e == nil {
		if len(data) > 0 {
			var n int
			n, e = f.Write(data)
			if e == nil && n != len(data) {
				e = io.ErrShortWrite
			}
		}
		zero := make([]byte, 16<<10)
		for left := size - int64(len(data)); left > 0 && e == nil; {
			amount := min(left, int64(len(zero)))
			var n int
			n, e = f.Write(zero[:amount])
			if e == nil && int64(n) != amount {
				e = io.ErrShortWrite
			}
			left -= amount
		}
	}
	if e == nil {
		e = fault.fail(name, "sync")
		if e == nil {
			e = f.Sync()
		}
	}
	ce := f.Close()
	fe := fault.fail(name, "close")
	if e != nil || ce != nil || fe != nil {
		return errStorageBootstrap
	}
	if e = fault.fail(name, "readback"); e != nil {
		return errStorageBootstrap
	}
	n, e := storageOpenFile(root, name, size)
	if e != nil {
		return errStorageBootstrap
	}
	buf := make([]byte, 16<<10)
	for offset := int64(0); offset < size && e == nil; {
		amount := min(size-offset, int64(len(buf)))
		var got int
		got, e = n.file.ReadAt(buf[:amount], offset)
		if e == nil && int64(got) != amount {
			e = io.ErrUnexpectedEOF
		}
		for i := int64(0); i < amount && e == nil; i++ {
			want := byte(0)
			if offset+i < int64(len(data)) {
				want = data[offset+i]
			}
			if buf[i] != want {
				e = errStorageBootstrap
			}
		}
		offset += amount
	}
	ce = n.file.Close()
	if e != nil || ce != nil {
		return errStorageBootstrap
	}
	return nil
}

// bootstrapStorageInstallation is the finite one-shot exception for an owner
// whose existing original home exclusion is available. It never resumes partial
// creation or claims to exclude an executable that ignores that original lease.
func bootstrapStorageInstallation(ctx context.Context, config, homeOwner string, opt storageBootstrapOptions, fault *storageBootstrapFault) (out *storageInstallationRef, err error) {
	o, e := storagePinOriginal(config)
	if e != nil {
		return nil, e
	}
	defer func() {
		if ce := o.close(); ce != nil {
			if out != nil {
				ce = errors.Join(ce, out.Close())
				out = nil
			}
			err = errors.Join(err, ce)
		}
	}()
	if e = lockPrivateData(o.lease, true); e != nil {
		return nil, storagebudget.ErrBusy
	}
	if e = o.current(); e != nil {
		return nil, e
	}
	// A journal in the original external location requires its original workflow.
	journal := homeJournalPath(config)
	journalParent := len(o.roots) - 2
	if filepath.Base(config) == deskHomePlatform {
		journalParent--
	}
	if journalParent < 0 {
		return nil, errStoragePrerequisite
	}
	if _, e = o.roots[journalParent].root.Lstat(filepath.Base(journal)); !errors.Is(e, os.ErrNotExist) {
		return nil, errStorageBootstrap
	}
	for _, name := range []string{storageClaimName, storageRecordName, storageBootstrapName, storageUseName, storageIntentName, storageReadyName, storageAnchorName} {
		if _, e = o.config.Lstat(name); !errors.Is(e, os.ErrNotExist) {
			return nil, errStorageBootstrap
		}
	}
	if e = storageBootstrapPreflight(ctx, o, opt); e != nil {
		return nil, e
	}
	create := func(root *os.Root, name string, data []byte, size int64) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := o.current(); e != nil {
			return e
		}
		if e := storageCreateControl(root, name, data, size, fault); e != nil {
			return e
		}
		n, e := storageOpenFile(root, name, size)
		if e != nil {
			return e
		}
		if name == storageClaimName || name == storageRecordName || name == storageReadyName {
			n.immutable = append([]byte(nil), data...)
		}
		o.files = append(o.files, n)
		return o.current()
	}
	id, e := storagebudget.NewID()
	if e != nil {
		return nil, storagebudget.ErrUnknown
	}
	nonce, e := storagebudget.NewID()
	if e != nil {
		return nil, storagebudget.ErrUnknown
	}
	configID, e := storageDirectoryID(o.config)
	if e != nil {
		return nil, e
	}
	claim := storageInstallationRecord{Version: 1, Kind: "claim", Installation: id, Nonce: nonce, Config: configID, HomeOwner: homeOwner}
	b, e := storageRecordBytes(claim)
	if e != nil {
		return nil, e
	}
	if e = create(o.config, storageClaimName, b, int64(len(b))); e != nil {
		return nil, e
	}
	// Persist the original claim's name before any dependent namespace growth.
	// Uncertain directory durability retains the claim and stops this protocol.
	if e = storageSyncRoot(o.config, "claim", fault); e != nil {
		return nil, e
	}
	for _, name := range []string{storageBootstrapName, storageUseName} {
		if e = create(o.config, name, nil, 0); e != nil {
			return nil, e
		}
	}
	bootstrap, e := storageOpenFile(o.config, storageBootstrapName, 0)
	if e != nil {
		return nil, e
	}
	o.files = append(o.files, bootstrap)
	if e = lockPrivateData(bootstrap.file, true); e != nil {
		return nil, storagebudget.ErrBusy
	}
	if e = o.current(); e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if e = fault.fail(storageAnchorName, "mkdir"); e != nil {
		return nil, errStorageBootstrap
	}
	if e = o.config.Mkdir(storageAnchorName, 0700); e != nil {
		return nil, errStorageBootstrap
	}
	anchor, e := storagePinChild(o.config, storageAnchorName, true)
	if e != nil {
		return nil, e
	}
	o.roots = append(o.roots, anchor)
	if e = create(anchor.root, "lock", nil, 0); e != nil {
		return nil, e
	}
	if e = create(o.config, storageIntentName, nil, storageIntentBytes); e != nil {
		return nil, e
	}
	record := claim
	record.Kind = "installation"
	record.Anchor, e = storageDirectoryID(anchor.root)
	if e != nil {
		return nil, e
	}
	b, e = storageRecordBytes(record)
	if e != nil {
		return nil, e
	}
	if e = create(o.config, storageRecordName, b, int64(len(b))); e != nil {
		return nil, e
	}
	binding := sha256.Sum256(append([]byte("desk-storage-installation-v1\x00"), b...))
	lock, e := storageOpenFile(anchor.root, "lock", 0)
	if e != nil {
		return nil, e
	}
	o.files = append(o.files, lock)
	q, e := storagebudget.OpenHeld(storagebudget.HeldAnchor{Root: anchor.root, LockWitness: lock.file, Installation: id, Binding: binding, AdvisoryLimits: storageLimitsAdvisory})
	if e != nil {
		return nil, e
	}
	initErr := fault.fail(storageAnchorName, "initialize")
	if initErr == nil {
		initErr = q.InitializeHeld(ctx, opt.Policy)
	}
	if initErr == nil {
		initErr = fault.fail(storageAnchorName, "initialized")
	}
	closeErr := q.Close()
	if initErr != nil || closeErr != nil {
		return nil, errStorageBootstrap
	}
	if e = storageSyncRoot(anchor.root, storageAnchorName, fault); e != nil {
		return nil, e
	}
	if e = storageSyncRoot(o.config, "config", fault); e != nil {
		return nil, e
	}
	if e = o.current(); e != nil {
		return nil, e
	}
	if e = create(o.config, storageReadyName, binding[:], 32); e != nil {
		return nil, e
	}
	if e = storageSyncRoot(o.config, "ready", fault); e != nil {
		return nil, e
	}
	// Reopen through exact same named custody, never initialize a second ledger.
	// Release bootstrap EX first; keep original home EX until the outer defer.
	if e = storageUnlock(bootstrap.file); e != nil {
		return nil, e
	}
	return openStorageInstallation(ctx, config, homeOwner)
}
