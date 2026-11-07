package desk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// One grant is consumed before its first effect, including uncertain failures.
// Q initialization is five exclusive files: 2*4MiB +2*1MiB +32MiB.
// Each Q checkpoint publication is one existing 4MiB file. Catalog writes are
// in-place 8MiB banks; creating both banks separately reserves their full length.
// No failed group can be retried during the same launch.
type storageOwnerBootstrapEffect struct{ bytes, files, directories, metadata uint64 }

var storageOwnerBootstrapEffects = map[string]storageOwnerBootstrapEffect{
	"config/claim": {4096, 1, 0, 0}, "config/record": {4096, 1, 0, 0},
	"config/bootstrap": {0, 1, 0, 0}, "config/use": {0, 1, 0, 0},
	"config/intent": {2097152, 1, 0, 0}, "config/catalog-a-create": {8388608, 1, 0, 0},
	"config/catalog-b-create": {8388608, 1, 0, 0}, "config/data-lock": {0, 1, 0, 0},
	"config/transfer-lock":     {0, 1, 0, 0},
	"config/transfer-a-create": {storageTransferBankBytes, 1, 0, 0},
	"config/transfer-b-create": {storageTransferBankBytes, 1, 0, 0},
	"config/work-lock":         {0, 1, 0, 0}, "config/anchor": {0, 0, 1, 0},
	"config/prepared": {8388608, 0, 0, 1}, "config/active": {8388608, 0, 0, 1},
	"config/ready": {32, 1, 0, 0}, "config/ready-complete": {0, 0, 0, 1},
	"anchor/lock": {0, 1, 0, 0}, "anchor/initialize": {44040192, 5, 0, 0},
	"anchor/register": {4194304, 0, 0, 1}, "anchor/maintenance": {4194304, 0, 0, 1},
	"anchor/census":         {4194304, 0, 0, 1},
	"anchor/complete-state": {44040192, 0, 0, 7},
	"config/claim-complete": {4096, 0, 0, 1}, "config/record-complete": {4096, 0, 0, 1},
}

func (c *storageBudgetConstruction) completeOwnerFile(ctx context.Context, n storageNamedFile, key string) (err error) {
	if err = n.current(); err != nil {
		return err
	}
	if err = c.reserveOwnerEffect(ctx, n.root, n.file, key); err != nil {
		return err
	}
	fresh, err := storageOpenFile(n.root, n.name, n.size)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, fresh.file.Close())
		}
	}()
	if !os.SameFile(fresh.identity, n.identity) {
		return errStorageCustody
	}
	if err = c.failOwner(key, "sync"); err != nil {
		return err
	}
	if err = fresh.file.Sync(); err != nil {
		return err
	}
	closed = true
	if err = errors.Join(fresh.file.Close(), c.failOwner(key, "close")); err != nil {
		return err
	}
	parent, err := n.root.Stat(".")
	if err != nil {
		return err
	}
	if err = c.failOwner(key, "parent-sync"); err != nil {
		return err
	}
	if err = errors.Join(storageSyncInfrastructureDescription(ctx, n.root, ".", parent), c.failOwner(key, "parent-close")); err != nil {
		return err
	}
	if err = c.failOwner(key, "readback"); err != nil {
		return err
	}
	return n.current()
}

func (c *storageBudgetConstruction) reserveOwnerEffect(ctx context.Context, parent *os.Root, target *os.File, key string) error {
	return c.reserveOwnerEffectForCensus(ctx, parent, target, key, nil)
}
func (c *storageBudgetConstruction) reserveOwnerEffectForCensus(ctx context.Context, parent *os.Root, target *os.File, key string, census *storageExclusiveCensusControl) error {
	check := func() error {
		if census != nil {
			if census.self != census || census.phase == nil || census.phase.c != c || parent != c.anchor || target != nil || key != "anchor/census" {
				return storagebudget.ErrAuthority
			}
			return census.current(ctx)
		}
		return c.requireConstructionMutation(ctx)
	}
	e, ok := storageOwnerBootstrapEffects[key]
	if !ok || c.grants[key] {
		return storagebudget.ErrBounds
	}
	if err := check(); err != nil {
		return err
	}
	if err := c.current(ctx, true); err != nil {
		return err
	}
	if parent != c.original.config && parent != c.anchor {
		return storagebudget.ErrAuthority
	}
	if census != nil {
		e.metadata = 28
	}
	demand, err := storageAdvisoryEstimate(e.bytes, e.files, e.directories, e.metadata)
	if err != nil {
		return err
	}
	// A finite whole-launch cumulative policy estimate is subtracted on every
	// exact parent view, including sharing that cannot be established natively.
	spent := c.spent
	if spent.Bytes > 1<<60-demand.Bytes || spent.Inodes > 1<<40-demand.Inodes {
		return storagebudget.ErrBounds
	}
	f, err := parent.Open(".")
	if err != nil {
		return err
	}
	var facts storageAdvisoryNativeFacts
	if census != nil {
		err = census.phase.boundary("allowance-native-read")
		if err == nil {
			err = census.phase.permit(ctx)
		}
	}
	if err == nil {
		facts, err = storageAdvisoryReadExistingHeld(ctx, f)
	}
	checkAvailability := func(facts storageAdvisoryNativeFacts) error {
		p := &storageInfrastructureStart{spent: spent}
		return p.checkAvailability(facts, demand)
	}
	if err == nil {
		err = checkAvailability(facts)
	}
	if err == nil && e.files+e.directories > 0 {
		facts, err = storageAdvisoryReadHeld(ctx, f)
		if err == nil {
			err = checkAvailability(facts)
		}
	}
	if err == nil && target != nil {
		facts, err = storageAdvisoryReadExistingLeaseHeld(ctx, f, target)
		if err == nil {
			err = checkAvailability(facts)
		}
	}
	if census != nil {
		err = errors.Join(err, census.phase.boundary("allowance-parent-close"))
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = c.current(ctx, true); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	if census != nil {
		if err = census.transition(ctx, &demand, false); err != nil {
			return err
		}
	} else {
		c.grants[key] = true
		c.spent.Bytes += demand.Bytes
		c.spent.Inodes += demand.Inodes
	}
	return c.failOwner(key, "before-effect")
}

// The six original anchor witnesses are distinct from config controls. Only
// this sealed launch can reopen them; neither a root nor an arbitrary control
// name is accepted from a caller. This does not initialize or repair Q.
func (c *storageBudgetConstruction) openOriginalAnchorControl(ctx context.Context, name string) (storageNamedFile, error) {
	if err := c.current(ctx, true); err != nil {
		return storageNamedFile{}, err
	}
	if c.ref == nil || !c.ref.valid() || c.ref.cell.original != c.original || c.ref.cell.anchor != c.anchor {
		return storageNamedFile{}, storagebudget.ErrAuthority
	}
	var size int64
	switch name {
	case "lock":
		size = 0
	case "checkpoint.a", "checkpoint.b":
		size = storagebudget.CheckpointBytes
	case "tickets.a", "tickets.b":
		size = storagebudget.Slots * storagebudget.TicketBytes
	case "manifests":
		size = storagebudget.Slots * storagebudget.ManifestBytes
	default:
		return storageNamedFile{}, storagebudget.ErrAuthority
	}
	for _, original := range c.original.files {
		if original.root != c.anchor || original.name != name {
			continue
		}
		if original.size != size {
			return storageNamedFile{}, errStorageCustody
		}
		if err := original.current(); err != nil {
			return storageNamedFile{}, err
		}
		fresh, err := storageOpenFile(c.anchor, name, size)
		if err != nil {
			return storageNamedFile{}, err
		}
		if err = errors.Join(original.current(), fresh.current()); err != nil || !os.SameFile(fresh.identity, original.identity) {
			return storageNamedFile{}, errors.Join(errStorageCustody, err, fresh.file.Close())
		}
		return fresh, nil
	}
	return storageNamedFile{}, errStorageCustody
}

func storageOwnerPersistentRoot(ctx context.Context, root *os.Root) (storageOwnerPersistentObject, error) {
	f, err := root.Open(".")
	if err != nil {
		return storageOwnerPersistentObject{}, err
	}
	p, err := storageAdvisoryPersistentHeld(ctx, f)
	return p, errors.Join(err, f.Close())
}
func storageOwnerPersistentFile(ctx context.Context, n storageNamedFile) (p storageOwnerPersistentObject, err error) {
	if err = n.current(); err != nil {
		return p, err
	}
	// The root is the original named-file parent already retained by the owner.
	// Open only that directory description, never a guessed target pathname.
	parent, err := n.root.Open(".")
	if err != nil {
		return p, err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	before, err := parent.Stat()
	named, nameErr := n.root.Stat(".")
	if err != nil || nameErr != nil || !before.IsDir() || !os.SameFile(before, named) {
		return p, errors.Join(errStorageCustody, err, nameErr)
	}
	p, err = storageAdvisoryPersistentFileHeld(ctx, parent, n.file)
	after, statErr := parent.Stat()
	current, currentErr := n.root.Stat(".")
	if statErr == nil && currentErr == nil && (!os.SameFile(before, after) || !os.SameFile(before, current)) {
		statErr = errStorageCustody
	}
	return p, errors.Join(err, statErr, currentErr, n.current())
}

// Exclusively create final names. All bytes are fixed, bounded public controls;
// no truncate, replacement, rollback/removal or unbounded retry is permitted.
func (c *storageBudgetConstruction) createOwnerFile(ctx context.Context, parent *os.Root, name, key string, raw []byte, size int64) (out storageNamedFile, err error) {
	if int64(len(raw)) > size || size < 0 || size > storageOwnerCatalogBytes {
		return out, storagebudget.ErrBounds
	}
	if err = c.reserveOwnerEffect(ctx, parent, nil, key); err != nil {
		return out, err
	}
	if _, e := parent.Lstat(name); !errors.Is(e, os.ErrNotExist) {
		return out, errors.Join(errStorageBootstrap, e)
	}
	f, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|openNoFollow|openNonBlocking, custodyFileMode)
	if err != nil {
		return out, err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, f.Close())
		}
	}()
	// At most128 writes of64KiB for a catalog; lengths come from the closed table.
	zero := make([]byte, 65536)
	if err = c.failOwner(key, "write"); err != nil {
		return out, err
	}
	for offset := int64(0); offset < size; {
		n := min(int64(len(zero)), size-offset)
		chunk := zero[:n]
		if offset < int64(len(raw)) {
			chunk = raw[offset:min(offset+n, int64(len(raw)))]
		}
		written, e := f.WriteAt(chunk, offset)
		if e != nil || written != len(chunk) {
			return out, errors.Join(io.ErrShortWrite, e)
		}
		offset += int64(written)
	}
	if err = c.failOwner(key, "sync"); err != nil {
		return out, err
	}
	if err = storageOwnerTransferControlSync(ctx, f, key); err != nil {
		return out, err
	}
	closed = true
	if err = f.Close(); err != nil {
		return out, err
	}
	if err = c.failOwner(key, "close"); err != nil {
		return out, err
	}
	info, err := parent.Stat(".")
	if err != nil {
		return out, err
	}
	if err = c.failOwner(key, "parent-sync"); err != nil {
		return out, err
	}
	if err = storageOwnerTransferControlParentSync(ctx, parent, info, key); err != nil {
		return out, err
	}
	if err = c.failOwner(key, "readback"); err != nil {
		return out, err
	}
	out, err = storageOpenFile(parent, name, size)
	if err != nil {
		return out, err
	}
	if size > 0 {
		b := make([]byte, size)
		if _, err = out.file.ReadAt(b, 0); err != nil || !bytes.Equal(raw, b[:len(raw)]) {
			return storageNamedFile{}, errors.Join(errStorageBootstrap, err, out.file.Close())
		}
		if !storageAllZero(b[len(raw):]) {
			return storageNamedFile{}, errors.Join(errStorageBootstrap, out.file.Close())
		}
	}
	if int64(len(raw)) == size && size > 0 {
		out.immutable = append([]byte(nil), raw...)
	}
	if err = out.current(); err != nil {
		return storageNamedFile{}, errors.Join(err, out.file.Close())
	}
	return out, nil
}

func (c *storageBudgetConstruction) retainOwnerFile(ctx context.Context, parent *os.Root, name string, size int64) (storageNamedFile, error) {
	n, err := storageOpenFile(parent, name, size)
	if err != nil {
		return n, err
	}
	c.original.files = append(c.original.files, n)
	return n, nil
}

func (c *storageBudgetConstruction) ownerGate(ctx context.Context, run func(context.Context) error) (err error) {
	ctx, err = c.constructionMutationContext(ctx)
	if err != nil {
		return err
	}
	if err = c.current(ctx, true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	n, err := storageOpenFile(c.original.config, storageBootstrapName, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, n.file.Close()) }()
	if err = lockPrivateData(n.file, true); err != nil {
		return storagebudget.ErrBusy
	}
	if err = n.current(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = run(ctx)
	return errors.Join(err, ctx.Err(), n.current())
}

func storageOwnerReadFixed(root *os.Root, name string, size int64) (raw []byte, n storageNamedFile, err error) {
	if _, err = root.Lstat(name); err != nil {
		return nil, n, err
	}
	n, err = storageOpenFile(root, name, size)
	if err != nil {
		return nil, n, err
	}
	raw = make([]byte, size)
	_, err = n.file.ReadAt(raw, 0)
	if err != nil {
		return nil, storageNamedFile{}, errors.Join(err, n.file.Close())
	}
	if err = n.current(); err != nil {
		return nil, storageNamedFile{}, errors.Join(err, n.file.Close())
	}
	return raw, n, nil
}

func (c *storageInstallation) currentOwnerV2() error {
	if c.startup == nil {
		return errStorageCustody
	}
	if err := c.startup.originCurrent(context.Background(), false); err != nil {
		return err
	}
	return c.ownerV2PhysicalCurrent()
}

// The caller first validates either fresh construction or an original retained
// installation use. Both paths run every catalogue/name/object check here.
func (c *storageInstallation) ownerV2PhysicalCurrent() error {
	r := c.ownerRecord
	if r == nil || r.validate() != nil || !c.ownerClaim.matches(*r) || c.binding != r.QBinding || c.record.Installation != r.Installation {
		return errStorageCustody
	}
	if err := c.original.current(); err != nil {
		return err
	}
	ctx := context.Background()
	if c.startup.sources != nil {
		if err := c.startup.sources.currentProjects(ctx); err != nil {
			return err
		}
	}
	for _, row := range []struct {
		root *os.Root
		want storageOwnerPersistentObject
	}{{c.original.config, r.Config}, {c.anchor, r.Anchor}} {
		got, err := storageOwnerPersistentRoot(ctx, row.root)
		if err != nil || got != row.want {
			return errors.Join(errStorageCustody, err)
		}
	}
	wants := map[string]storageOwnerPersistentObject{storageBootstrapName: r.BootstrapLock, storageUseName: r.UseLock, ".home-use.lock": r.HomeLease, "storage-owner.a": r.CatalogA, "storage-owner.b": r.CatalogB}
	for name, want := range wants {
		found := false
		for _, n := range c.original.files {
			if n.root == c.original.config && n.name == name {
				got, err := storageOwnerPersistentFile(ctx, n)
				if err != nil || got != want {
					return errors.Join(errStorageCustody, err)
				}
				found = true
			}
		}
		if !found {
			return errStorageCustody
		}
	}
	// Ready is lexical content, never a permanent inode identity. During sealed
	// startup it may be absent; no request enrollment is exposed until publication.
	readyRetained := false
	for _, original := range c.original.files {
		if original.root == c.original.config && original.name == storageReadyName {
			readyRetained = true
		}
	}
	if c.ownerCatalog.Phase == "active" || readyRetained || c.enrollmentEndpoint != nil {
		b, n, err := storageOwnerReadFixed(c.original.config, storageReadyName, 32)
		if err != nil {
			return err
		}
		err = errors.Join(n.current(), n.file.Close())
		if !bytes.Equal(b, c.binding[:]) {
			return errors.Join(errStorageCustody, err)
		}
		return err
	}
	return nil
}

// Only the new closed transfer controls claim the checked native barrier.
// Historical bootstrap paths retain their existing protocol and bytes.
func storageOwnerTransferControlKey(key string) bool {
	return key == "config/transfer-lock" || key == "config/transfer-a-create" || key == "config/transfer-b-create"
}
func storageOwnerTransferControlSync(ctx context.Context, f *os.File, key string) error {
	if !storageOwnerTransferControlKey(key) {
		return f.Sync()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return storageEnrollmentNativeSync(ctx, f)
}
func storageOwnerTransferControlParentSync(ctx context.Context, root *os.Root, want os.FileInfo, key string) (err error) {
	if !storageOwnerTransferControlKey(key) {
		return storageSyncInfrastructureDescription(ctx, root, ".", want)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	held, err := f.Stat()
	named, e := root.Stat(".")
	if err != nil || e != nil || !os.SameFile(held, want) || !os.SameFile(named, want) {
		return errors.Join(errStorageCustody, err, e)
	}
	if err = storageEnrollmentNativeSync(ctx, f); err != nil {
		return err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(after, want) {
		return errors.Join(errStorageCustody, err)
	}
	return ctx.Err()
}

// Fresh original construction has two existing 8MiB catalog banks in addition
// to the unchanged covered provision. The transfer pair/lock are measured
// independently, not hidden inside storageHostBudget or Q's fixed extent.
func storageTransferBootstrapPolicyRoom(policy storagebudget.Policy) error {
	floor := storagebudget.Vector{Bytes: storagebudget.MetadataBudget + storageHostBudget + 2*storageOwnerCatalogBytes + 2*storageTransferBankBytes, Files: 37, Directories: 5}
	fits := func(limit storagebudget.Vector) bool {
		return floor.Bytes <= limit.Bytes && floor.Files <= limit.Files && floor.Directories <= limit.Directories
	}
	if !fits(policy.Installation.Total) || !fits(policy.Installation.Category[storagebudget.Control]) {
		return storagebudget.ErrLimit
	}
	return nil
}

// The policy's two catalog groups rewrite only the original fixed banks. No
// startup grant is reset or borrowed, and no create/rename/truncate is issued.
// Close and parent barrier are owned by this invocation even after cancellation.
func (m *storagePolicyOperation) publish(ctx context.Context, group int, raw []byte) error {
	return m.catalog(ctx, group, raw, true)
}

// Recovery persists already-matched original bytes without replaying a write.
func (m *storagePolicyOperation) persistCatalog(ctx context.Context, group int, raw []byte) error {
	return m.catalog(ctx, group, raw, false)
}

func (m *storagePolicyOperation) catalog(ctx context.Context, group int, raw []byte, write bool) error {
	if m == nil || m.phase == nil || group != 0 && group != 3 || len(raw) != storageOwnerCatalogBytes || group == 3 && !m.consumed[0] {
		return storagebudget.ErrAuthority
	}
	bank := m.bank
	if group == 3 {
		bank = 1 - bank
	}
	if bank > 1 {
		return storagebudget.ErrAuthority
	}
	if group == 0 && !bytes.Equal(raw, m.prepared) || group == 3 && !bytes.Equal(raw, m.active) && !bytes.Equal(raw, m.aborted) && !bytes.Equal(raw, m.unfenced) {
		return storagebudget.ErrAuthority
	}
	if err := m.nativeCurrent(ctx); err != nil {
		return err
	}
	n, err := storageOpenOriginalControl(m.phase.c.ref.cell, []string{"storage-owner.a", "storage-owner.b"}[bank])
	if err != nil {
		return err
	}
	entered := false
	closed := false
	run := func() (err error) {
		defer func() {
			if !closed {
				err = errors.Join(err, n.file.Close())
			}
		}()
		if err = m.take(ctx, group); err != nil {
			return err
		}
		entered = true
		if !write {
			before := make([]byte, len(raw))
			count, e := n.file.ReadAt(before, 0)
			if e != nil || count != len(before) || !bytes.Equal(before, raw) {
				return errors.Join(errStorageCustody, e)
			}
		} else {
			for offset := 0; offset < len(raw); offset += 65536 {
				if err = ctx.Err(); err != nil {
					return err
				}
				if err = n.current(); err != nil {
					return err
				}
				chunk := raw[offset : offset+65536]
				count, e := n.file.WriteAt(chunk, int64(offset))
				if e != nil || count != len(chunk) {
					return errors.Join(io.ErrShortWrite, e)
				}
			}
		}
		if err = m.phase.boundary([]string{"policy-prepared-sync", "", "", "policy-final-sync"}[group]); err != nil {
			return err
		}
		if err = storageOwnerTransferControlSync(ctx, n.file, "policy-catalog"); err != nil {
			return err
		}
		closed = true
		if err = n.file.Close(); err != nil {
			return err
		}
		info, err := n.root.Stat(".")
		if err != nil {
			return err
		}
		if err = storageOwnerTransferControlParentSync(ctx, n.root, info, "policy-catalog"); err != nil {
			return err
		}
		fresh, err := storageOpenOriginalControl(m.phase.c.ref.cell, n.name)
		if err != nil {
			return err
		}
		body := make([]byte, storageOwnerCatalogBytes)
		count, e := fresh.file.ReadAt(body, 0)
		e = errors.Join(e, fresh.current(), fresh.file.Close())
		if e != nil || count != len(body) || !bytes.Equal(body, raw) {
			return errors.Join(errStorageCustody, e)
		}
		return nil
	}
	err = run()
	if entered {
		return m.phase.leaveNative(err)
	}
	return err
}

// Q owns every checkpoint mutation. Recovery may only finish persistence of
// the still-present exact selected original; it never writes/replays Q bytes.
func (m *storagePolicyOperation) persistQ(ctx context.Context, group int, phase storagebudget.PolicyPhase) (err error) {
	if group != 1 && group != 2 && group != 4 {
		return storagebudget.ErrAuthority
	}
	q := m.phase.c.ref.cell.q
	if err = q.CheckPolicyTransition(ctx, m.transition, phase); err != nil {
		return err
	}
	bank := m.transition.Original.CheckpointBank
	if phase == storagebudget.PolicyQ1 || phase == storagebudget.PolicyQ3 {
		bank = 1 - bank
	}
	n, err := m.phase.openEnrollmentCheckpoint(ctx, []string{"checkpoint.a", "checkpoint.b"}[bank])
	if err != nil {
		return err
	}
	closed, entered := false, false
	defer func() {
		if !closed {
			err = errors.Join(err, n.file.Close())
		}
		if entered {
			err = m.phase.leaveNative(err)
		}
	}()
	if err = m.take(ctx, group); err != nil {
		return err
	}
	entered = true
	if err = storageEnrollmentNativeSync(ctx, n.file); err != nil {
		return err
	}
	closed = true
	if err = n.file.Close(); err != nil {
		return err
	}
	return q.CheckPolicyTransition(ctx, m.transition, phase)
}

// A later ordinary Q revision does not replay an older policy checkpoint.
// Startup persists the actual selected current checkpoint in the original
// fifth group, comparing the complete same-load raw state before and after.
func (m *storagePolicyOperation) persistCompletedQ(ctx context.Context, observed storagebudget.EnrollmentInspection) (err error) {
	if err = storagePolicyCompletedQ(m.original, observed); err != nil {
		return err
	}
	name := "checkpoint.a"
	if observed.CheckpointBank == 1 {
		name = "checkpoint.b"
	}
	n, err := m.phase.openEnrollmentCheckpoint(ctx, name)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, n.current(), n.file.Close()) }()
	before, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil || !reflect.DeepEqual(before, observed) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	if err = m.take(ctx, 4); err != nil {
		return err
	}
	err = storageOwnerTransferControlSync(ctx, n.file, "policy-checkpoint")
	err = m.phase.leaveNative(err)
	if err != nil {
		return err
	}
	after, err := m.phase.c.ref.cell.q.InspectAction(ctx)
	if err != nil || !reflect.DeepEqual(after, observed) {
		return errors.Join(errStorageEnrollmentUnresolved, err)
	}
	return nil
}
