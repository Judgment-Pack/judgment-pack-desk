package desk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func (c *storageBudgetConstruction) loadOrEnrollOwner(ctx context.Context) (err error) {
	defer func() {
		if err != nil && c != nil && c.mutation != nil {
			err = errors.Join(err, c.mutation.Close())
		}
	}()
	if err = c.current(ctx, true); err != nil {
		return err
	}
	configPO, err := storageOwnerPersistentRoot(ctx, c.original.config)
	if err != nil {
		return err
	}
	profile, err := storageHostInstalledProfile(configPO.Backend)
	if err != nil {
		return err
	}
	if err = storageOwnerExactInstalledProfiles([]storageOwnerProfile{profile}); err != nil {
		return err
	}
	profileDigest, err := storageOwnerDigest("desk-storage-owner-profile-set-v2", storageOwnerProfileSetMaterial{Profiles: []storageOwnerProfile{profile}})
	if err != nil {
		return err
	}
	var claim storageOwnerClaimV2
	b, cn, readErr := storageOwnerReadImmutable(ctx, c.original.config, storageClaimName)
	if errors.Is(readErr, os.ErrNotExist) {
		// An absent claim is fresh only when every owner authority/control name is
		// positively absent. Existing pre-budget data is deliberately untouched.
		for _, name := range []string{storageRecordName, storageAnchorName, storageBootstrapName, storageUseName, storageIntentName, "storage-owner.a", "storage-owner.b", storageReadyName, ".data-transfer.lock", storageTransferBankA, storageTransferBankB} {
			if _, e := c.original.config.Lstat(name); !errors.Is(e, os.ErrNotExist) {
				return errors.Join(errStorageBootstrap, e)
			}
		}
		if err = storageTransferBootstrapPolicyRoom(storageHostInitialPolicy()); err != nil {
			return err
		}
		installation, e := storagebudget.NewID()
		if e != nil {
			return e
		}
		nonce, e := storagebudget.NewID()
		if e != nil {
			return e
		}
		claim = storageOwnerClaimV2{Version: 2, Kind: "claim", Installation: installation, Nonce: nonce, HomeOwner: c.server.installationOwner(), Config: configPO, ProfileSetDigest: profileDigest}
		b, err = storageOwnerEncodeClaimV2(claim)
		if err != nil {
			return err
		}
		cn, err = c.createOwnerFile(ctx, c.original.config, storageClaimName, "config/claim", b, storageOwnerRecordBytes)
		if err != nil {
			return err
		}
		cn.immutable = append([]byte(nil), b...)
	} else if readErr != nil {
		return readErr
	} else {
		claim, err = storageOwnerDecodeClaimV2(b)
		if err != nil {
			return errors.Join(errStorageBootstrap, err, cn.file.Close())
		}
		if claim.Config != configPO || claim.HomeOwner != c.server.installationOwner() || claim.ProfileSetDigest != profileDigest {
			return errors.Join(errStorageCustody, cn.file.Close())
		}
	}
	c.original.files = append(c.original.files, cn)
	if !c.grants["config/claim"] {
		if err = c.completeOwnerFile(ctx, cn, "config/claim-complete"); err != nil {
			return err
		}
	}
	var record storageOwnerRecordV2
	b, rn, readErr := storageOwnerReadImmutable(ctx, c.original.config, storageRecordName)
	newRecord := errors.Is(readErr, os.ErrNotExist)
	if readErr != nil && !newRecord {
		return readErr
	}
	if !newRecord {
		record, err = storageOwnerDecodeRecordV2(b)
		if err != nil || !claim.matches(record) {
			return errors.Join(errStorageBootstrap, err, rn.file.Close())
		}
		c.original.files = append(c.original.files, rn)
		if err = c.completeOwnerFile(ctx, rn, "config/record-complete"); err != nil {
			return err
		}
	}
	if newRecord {
		if err = c.validateBootstrapContinuation(ctx); err != nil {
			return err
		}
	}
	// A known original claim may continue a positively absent dependent step.
	// Present malformed or uncertain objects are never truncated/reinitialized.
	for _, row := range []struct {
		name, key string
		size      int64
	}{{storageBootstrapName, "config/bootstrap", 0}, {storageUseName, "config/use", 0}, {storageIntentName, "config/intent", storageIntentBytes}, {"storage-owner.a", "config/catalog-a-create", storageOwnerCatalogBytes}, {"storage-owner.b", "config/catalog-b-create", storageOwnerCatalogBytes}, {".data.lock", "config/data-lock", 0}, {".data-work.lock", "config/work-lock", 0}} {
		n, e := storageOpenFile(c.original.config, row.name, row.size)
		if e != nil {
			if _, le := c.original.config.Lstat(row.name); !newRecord || !errors.Is(le, os.ErrNotExist) {
				return errors.Join(errStorageBootstrap, e)
			}
			n, e = c.createOwnerFile(ctx, c.original.config, row.name, row.key, nil, row.size)
			if e != nil {
				return e
			}
		}
		c.original.files = append(c.original.files, n)
	}
	// These original controls form one closed feature set. An existing owner
	// without the feature remains readable; a partial set cannot be adopted or
	// filled during an ordinary launch. Only the original fresh claim may create.
	if err = c.retainWholeTransferControls(ctx, newRecord); err != nil {
		return err
	}
	anchor, e := storagePinChild(c.original.config, storageAnchorName, true)
	if e != nil {
		if _, le := c.original.config.Lstat(storageAnchorName); !newRecord || !errors.Is(le, os.ErrNotExist) {
			return errors.Join(errStorageBootstrap, e)
		}
		if e = c.reserveOwnerEffect(ctx, c.original.config, nil, "config/anchor"); e != nil {
			return e
		}
		if e = c.original.config.Mkdir(storageAnchorName, custodyDirMode); e != nil {
			return e
		}
		anchor, e = storagePinChild(c.original.config, storageAnchorName, true)
		if e != nil {
			return e
		}
		c.original.roots = append(c.original.roots, anchor)
		c.anchor = anchor.root
		if e = storageSyncInfrastructureDescription(ctx, c.original.config, storageAnchorName, anchor.identity); e != nil {
			return e
		}
		parentInfo, e := c.original.config.Stat(".")
		if e != nil {
			return e
		}
		if e = storageSyncInfrastructureDescription(ctx, c.original.config, ".", parentInfo); e != nil {
			return e
		}
	} else {
		c.original.roots = append(c.original.roots, anchor)
		c.anchor = anchor.root
	}
	lock, e := storageOpenFile(c.anchor, "lock", 0)
	if e != nil {
		if _, le := c.anchor.Lstat("lock"); !newRecord || !errors.Is(le, os.ErrNotExist) {
			return errors.Join(errStorageBootstrap, e)
		}
		lock, e = c.createOwnerFile(ctx, c.anchor, "lock", "anchor/lock", nil, 0)
		if e != nil {
			return e
		}
	}
	c.original.files = append(c.original.files, lock)
	if newRecord {
		// No record means no prior Q admission can be proved. Only the stable
		// lock may exist before the immutable binding is first published.
		if e = storageOwnerAnchorOnlyLock(c.anchor); e != nil {
			return e
		}
		record = storageOwnerRecordV2{Version: 2, Kind: "installation", Installation: claim.Installation, Nonce: claim.Nonce, HomeOwner: claim.HomeOwner, BindingVersion: 2, Config: configPO, ProfileSetDigest: claim.ProfileSetDigest}
		record.Anchor, e = storageOwnerPersistentRoot(ctx, c.anchor)
		if e != nil {
			return e
		}
		for _, n := range c.original.files {
			if n.root != c.original.config {
				continue
			}
			p, pe := storageOwnerPersistentFile(ctx, n)
			if pe != nil {
				return pe
			}
			switch n.name {
			case storageBootstrapName:
				record.BootstrapLock = p
			case storageUseName:
				record.UseLock = p
			case ".home-use.lock":
				record.HomeLease = p
			case "storage-owner.a":
				record.CatalogA = p
			case "storage-owner.b":
				record.CatalogB = p
			}
		}
		record.BindingMaterialDigest, record.QBinding, e = storageOwnerRecordDigests(record)
		if e != nil {
			return e
		}
		b, e = storageOwnerEncodeRecordV2(record)
		if e != nil {
			return e
		}
		rn, e = c.createOwnerFile(ctx, c.original.config, storageRecordName, "config/record", b, storageOwnerRecordBytes)
		if e != nil {
			return e
		}
		rn.immutable = append([]byte(nil), b...)
		c.original.files = append(c.original.files, rn)
	}
	anchorOnlyLock, e := storageOwnerAnchorState(c.anchor)
	if e != nil {
		return e
	}
	if anchorOnlyLock {
		if _, e = c.original.config.Lstat(storageReadyName); !errors.Is(e, os.ErrNotExist) {
			return errors.Join(errStorageBootstrap, e)
		}
		if e = c.requireBlankCatalogs(ctx); e != nil {
			return e
		}
	}
	peak, peakCatalogs, e := c.selectStartupEnrollment(ctx, record, claim)
	if e != nil {
		return e
	}
	if peak == nil && !c.startupComposed && !storagePolicyActualPair(c.startupPhysical) {
		if e = c.validateStartupHostJournal(ctx, record, anchorOnlyLock); e != nil {
			return e
		}
	}
	q, e := storagebudget.OpenHeld(storagebudget.HeldAnchor{Root: c.anchor, LockWitness: lock.file, Installation: record.Installation, Binding: record.QBinding})
	if e != nil {
		return e
	}
	// The legacy record projection contains only current-launch census witness
	// numbers. Original authority/binding is exclusively ownerRecord v2.
	configLive, e := storageDirectoryID(c.original.config)
	if e != nil {
		return errors.Join(e, q.Close())
	}
	anchorLive, e := storageDirectoryID(c.anchor)
	if e != nil {
		return errors.Join(e, q.Close())
	}
	c.ref = storageOwner(c.original, c.anchor, storageInstallationRecord{Version: 2, Installation: record.Installation, Nonce: record.Nonce, Config: configLive, Anchor: anchorLive, HomeOwner: record.HomeOwner}, b, record.QBinding, q)
	c.ref.cell.ownerRecord = &record
	c.ref.cell.ownerClaim = claim
	c.ref.cell.startup = c
	if e = c.ref.cell.currentOwnerV2(); e != nil {
		return e
	}
	if anchorOnlyLock {
		if e = c.reserveOwnerEffect(ctx, c.anchor, nil, "anchor/initialize"); e != nil {
			return e
		}
		if e = c.requireConstructionMutation(ctx); e != nil {
			return e
		}
		if e = q.InitializeHeld(ctx, storageHostInitialPolicy()); e != nil {
			return errors.Join(errStorageBootstrap, e)
		}
	} else {
		// Any partial/invalid initialized Q is read/refused by InspectHost. There
		// is no InitializeHeld retry, highest-page fallback or zero-fill route.
		if _, e = q.InspectHost(ctx); e != nil {
			return errors.Join(errStorageBootstrap, e)
		}
	}
	for _, row := range []struct {
		name string
		size int64
	}{{"checkpoint.a", storagebudget.CheckpointBytes}, {"checkpoint.b", storagebudget.CheckpointBytes}, {"tickets.a", storagebudget.Slots * storagebudget.TicketBytes}, {"tickets.b", storagebudget.Slots * storagebudget.TicketBytes}, {"manifests", storagebudget.Slots * storagebudget.ManifestBytes}} {
		if _, e = c.retainOwnerFile(ctx, c.anchor, row.name, row.size); e != nil {
			return e
		}
	}
	if e = c.validateStartupCompositionQ(ctx, record, claim); e != nil {
		return e
	}
	if storagePolicyActualPair(c.startupPhysical) {
		return c.resumeOriginalPolicyStartup(ctx, record, claim)
	}
	// An ordinary start selects its saved catalog before inspecting sources. The
	// selection is the same read-only bank read under the owner gate; source
	// inspection uses it only to tell an optional registered child from one
	// made after registration. The exact comparison below is unchanged.
	var cat storageOwnerCatalogV2
	var bank int
	var raw []byte
	var reattached *storageOwnerCatalogV2
	if peak == nil {
		cat, bank, raw, e = c.selectOwnerCatalog(ctx, record, claim)
		if e != nil {
			return e
		}
		if raw != nil {
			reattached = &cat
		}
	}
	x, e := c.inspectOriginalSources(ctx, record.Installation, reattached)
	if e != nil {
		return e
	}
	c.sources = x
	if e = x.buildObjects(ctx); e != nil {
		return e
	}
	if peak != nil {
		return c.resumeOriginalEnrollmentStartup(ctx, *peak, peakCatalogs, record, claim)
	}
	if raw != nil {
		if e = storageOwnerSameStartupProject(cat, x); e != nil {
			return e
		}
	}
	freshCatalog := raw == nil
	if freshCatalog {
		v, ve := q.InspectHost(ctx)
		if ve != nil {
			return ve
		}
		if len(v.Roots)+len(v.Workspaces)+len(v.Configuration.Filesystems)+len(v.Configuration.CapacityConstraints) != 0 || !v.Configuration.Maintenance || v.Configuration.Epoch != 1 || v.Configuration.Revision != 1 || v.Configuration.Policy != storageHostInitialPolicy() {
			return errStorageOwnerRecovery
		}
		for _, slot := range v.Slots {
			if slot.Ticket.State != storagebudget.Empty {
				return errStorageOwnerRecovery
			}
		}
		cat, e = c.constructOwnerCatalog(ctx, x, record, claim, profile, v)
		if e != nil {
			return e
		}
		raw, e = storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(cat), record, claim)
		if e != nil {
			return e
		}
		bank = 0
	}
	if e = storageOwnerExactInstalledProfiles(cat.Profiles); e != nil {
		return e
	}
	if !reflect.DeepEqual(cat.OriginalOwner, x.owner) {
		return errStorageCustody
	}
	if !reflect.DeepEqual(cat.Objects, x.objects) || !reflect.DeepEqual(cat.OwnerControls, x.controls) {
		return errStorageCustody
	}
	if len(cat.Workspaces) != len(x.workspaces) {
		return errStorageOwnerRecovery
	}
	for _, old := range cat.Workspaces {
		found := false
		for _, now := range x.workspaces {
			if old.OwnerKind == now.OwnerKind && bytes.Equal(old.OwnerKey, now.OwnerKey) && bytes.Equal(old.HistoryIdentity, now.HistoryIdentity) {
				found = true
				break
			}
		}
		if !found {
			return errStorageOwnerRecovery
		}
	}
	// Startup reattaches every catalog row and refuses additions or omissions;
	// source changes require their original obligation-conserving transition.
	if len(cat.Roots) != len(x.roots) {
		return errStorageOwnerRecovery
	}
	for _, row := range cat.Roots {
		if _, e = x.exactRoot(ctx, cat, row); e != nil {
			return e
		}
	}
	// Install the concrete observer and exact original rows before acquiring
	// use/domain EX. No short control gate is held while these prefixes open.
	install := cat
	install.Phase = "prepared"
	candidate, e := newStorageAdvisoryBudgetOwner(c.ref, install, record, claim, storageBudgetOptions{})
	if e != nil {
		return e
	}
	if e = c.installAdmissionOwner(ctx, candidate); e != nil {
		return errors.Join(e, candidate.Close())
	}
	ctx = storageMutationContext(ctx, c.mutation)
	if e = c.attachOwnerBindings(ctx, cat, x); e != nil {
		return e
	}
	defer func() {
		// The outer finalizer alone closes original startup holds, after the
		// invocation has drained. Construction Close joins this exact finish.
		cleanupErr := c.releaseStartupHolds()
		err = errors.Join(err, cleanupErr)
		c.mu.Lock()
		if c.exclusiveCleanupDone != nil {
			c.exclusiveCleanupErr = cleanupErr
			close(c.exclusiveCleanupDone)
		}
		c.mu.Unlock()
	}()
	if c.startupComposed || c.startupLocal {
		// Original action recovery and physical founding/lineage persistence precede
		// every ordinary census. The retained original Q observation is still exact.
		c.ref.cell.ownerCatalog = cat
		demand, e0 := c.originalIdentityDemand(ctx)
		if e0 != nil {
			return e0
		}
		if demand {
			e = c.completeOriginalIdentity(ctx)
		} else {
			e = c.completeOriginalLocalCreation(ctx)
		}
		if e != nil {
			return e
		}
		// Child recovery cannot discharge the primary's later automatic demand.
	}
	if e = c.acquireStartupHolds(ctx, cat); e != nil {
		return e
	}
	if !c.grants["anchor/initialize"] {
		if e = c.completeQPersistence(ctx); e != nil {
			return e
		}
	}
	if freshCatalog {
		if e = c.publishOwnerCatalog(ctx, bank, raw, "config/prepared"); e != nil {
			return e
		}
	}
	if !c.startupComposed {
		if e = c.completeOwnerTransaction(ctx, &cat, bank, raw); e != nil {
			return e
		}
	}
	if e = c.publishOwnerReady(ctx, cat); e != nil {
		return e
	}
	c.ref.cell.ownerCatalog = cat
	if c.exclusiveCensusEligible {
		return c.runExclusiveCensus(ctx)
	} else {
		e = c.owner.initialCensusFromStartup(ctx, c.home)
	}
	if e != nil {
		return e
	}
	return c.ref.cell.current()
}

func storageOwnerAnchorOnlyLock(root *os.Root) (err error) {
	d, err := root.Open(".")
	if err != nil {
		return err
	}
	names, re := d.ReadDir(7)
	err = errors.Join(re, d.Close())
	if err != nil {
		return err
	}
	if len(names) != 1 || names[0].Name() != "lock" {
		return errStorageBootstrap
	}
	return nil
}

func (c *storageBudgetConstruction) validateBootstrapContinuation(ctx context.Context) error {
	if _, err := c.original.config.Lstat(storageReadyName); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errStorageBootstrap, err)
	}
	for _, row := range []struct {
		name string
		size int64
		zero bool
	}{{storageBootstrapName, 0, false}, {storageUseName, 0, false}, {storageIntentName, storageIntentBytes, true}, {"storage-owner.a", storageOwnerCatalogBytes, true}, {"storage-owner.b", storageOwnerCatalogBytes, true}, {".data.lock", 0, false}, {".data-work.lock", 0, false}, {".data-transfer.lock", 0, false}, {storageTransferBankA, storageTransferBankBytes, true}, {storageTransferBankB, storageTransferBankBytes, true}} {
		if _, err := c.original.config.Lstat(row.name); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		n, err := storageOpenFile(c.original.config, row.name, row.size)
		if err != nil {
			return err
		}
		if row.zero {
			b := make([]byte, 65536)
			for offset := int64(0); offset < row.size; offset += int64(len(b)) {
				if err = ctx.Err(); err != nil {
					break
				}
				var read int
				chunk := b[:min(int64(len(b)), row.size-offset)]
				read, err = n.file.ReadAt(chunk, offset)
				if err != nil || read != len(chunk) {
					break
				}
				if !storageAllZero(chunk) {
					err = errStorageBootstrap
				}
				if err != nil {
					break
				}
			}
		}
		err = errors.Join(err, n.current(), n.file.Close())
		if err != nil {
			return err
		}
	}
	if _, err := c.original.config.Lstat(storageAnchorName); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	n, err := storagePinChild(c.original.config, storageAnchorName, true)
	if err != nil {
		return err
	}
	d, e := n.root.Open(".")
	if e != nil {
		return errors.Join(e, n.root.Close())
	}
	names, re := d.ReadDir(2)
	if len(names) == 0 && errors.Is(re, io.EOF) {
		re = nil
	}
	err = errors.Join(re, d.Close())
	if err == nil && !(len(names) == 0 || len(names) == 1 && names[0].Name() == "lock") {
		err = errStorageBootstrap
	}
	return errors.Join(err, n.root.Close())
}

// errDeskAnotherStartupProject refuses a start on another project in an
// installation without a Desk home. Its storage catalog is sealed to the
// project Desk was first started on (storage-budget-startup.md: the original
// adopted project seals construction), and no transition moves that owner to
// another project, so the start would otherwise run read-only with a generic
// pause. Desk does not start; nothing is reconciled or merged.
var errDeskAnotherStartupProject = errors.New("This installation's storage belongs to the project Desk was first started on with these settings and data folders, not to this project. Start Desk on that project, or move that project from there with Move… under Admin → General → Desk home.")

// errDeskProjectMovedOutside refuses a start on a project folder that was
// moved or renamed outside Desk after storage registered it. The owner record
// names the folder at its earlier path, so the start would otherwise refuse
// custody and run read-only with a generic pause. Desk does not start; nothing
// is rewritten. Following a folder by its identity is Desk #698.
var errDeskProjectMovedOutside = errors.New("project moved outside Desk")

type storageProjectMovedOutsideError struct{ From, To string }

func (e storageProjectMovedOutsideError) Error() string {
	return "This project was moved outside Desk, from " + e.From + " to " + e.To + ". Desk registered its storage at the earlier folder and cannot follow a move it did not make. Move the folder back to " + e.From + " and start Desk there; to move it, use Move… under Admin → General → Desk home."
}
func (e storageProjectMovedOutsideError) Is(target error) bool {
	return target == errDeskProjectMovedOutside || target == errStorageCustody
}

// A saved catalog without a Desk home records exactly one project workspace:
// the startup project's, keyed by its project identity. A start whose project
// has another identity key is another project. A Desk home's registry projects
// and every other difference, including the same project's history identity,
// keep the exact comparison that follows.
func storageOwnerSameStartupProject(cat storageOwnerCatalogV2, x *storageOriginalSources) error {
	if x == nil || cat.OriginalOwner.Kind != 1 || x.owner.Kind != 1 || !bytes.Equal(cat.OriginalOwner.ConfigPath, x.owner.ConfigPath) || len(cat.Workspaces) != 1 || len(x.workspaces) != 1 {
		return nil
	}
	old, now := cat.Workspaces[0], x.workspaces[0]
	if old.OwnerKind != 3 || now.OwnerKind != 3 || bytes.Equal(old.OwnerKey, now.OwnerKey) {
		return nil
	}
	return errDeskAnotherStartupProject
}

func (c *storageBudgetConstruction) selectOwnerCatalog(ctx context.Context, r storageOwnerRecordV2, claim storageOwnerClaimV2) (cat storageOwnerCatalogV2, bank int, frame []byte, err error) {
	if c.startupComposed {
		pair, e := storageOwnerDecodeCatalogPairV3(c.startupCores[0], c.startupCores[1], r, claim)
		if e != nil || pair.High.Phase != "active" {
			return cat, 0, nil, errors.Join(errStorageEnrollmentUnresolved, e)
		}
		bank = 0
		peer, e := storageOwnerDecodeCatalogV3(c.startupCores[1], r, claim)
		if e != nil {
			return cat, 0, nil, e
		}
		if peer.Sequence == pair.High.Sequence {
			bank = 1
		}
		return pair.High, bank, append([]byte{}, c.startupCores[bank]...), nil
	}
	var raw [2][]byte
	var valid [2]bool
	var decoded [2]storageOwnerCatalogV2
	err = c.ownerGate(ctx, func(ctx context.Context) error {
		for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
			b, n, e := storageOwnerReadFixed(c.original.config, name, storageOwnerCatalogBytes)
			if e != nil {
				return e
			}
			if e = errors.Join(n.current(), n.file.Close()); e != nil {
				return e
			}
			raw[i] = b
			v, e := storageOwnerDecodeCatalogV3(b, r, claim)
			if e == nil {
				valid[i] = true
				decoded[i] = storageOwnerCatalogV2(v)
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	for _, b := range raw {
		if len(b) >= 8 && (string(b[:8]) == "DSKONP1\x00" || storageEnrollmentPeakKnownOld(b)) {
			return cat, 0, nil, errStorageEnrollmentUnresolved
		}
	}
	if !valid[0] && !valid[1] {
		if bytes.Equal(raw[0], make([]byte, storageOwnerCatalogBytes)) && bytes.Equal(raw[1], raw[0]) {
			return cat, 0, nil, nil
		}
		return cat, 0, nil, errStorageOwnerRecovery
	}
	if valid[0] && valid[1] {
		pair, e := storageOwnerDecodeCatalogPairV3(raw[0], raw[1], r, claim)
		if e != nil {
			return cat, 0, nil, errors.Join(errStorageOwnerRecovery, e)
		}
		cat = pair.High
		bank = 0
		if decoded[1].Sequence == cat.Sequence {
			bank = 1
		}
	} else {
		bank = 0
		if valid[1] {
			bank = 1
		}
		cat = decoded[bank]
	}
	// Selection remains provisional until actual Q Before/After classification.
	return cat, bank, raw[bank], nil
}

func (c *storageBudgetConstruction) publishOwnerCatalog(ctx context.Context, bank int, raw []byte, key string) error {
	if bank < 0 || bank > 1 || len(raw) != storageOwnerCatalogBytes {
		return storagebudget.ErrBounds
	}
	return c.ownerGate(ctx, func(ctx context.Context) (err error) {
		name := []string{"storage-owner.a", "storage-owner.b"}[bank]
		n, err := storageOpenFile(c.original.config, name, storageOwnerCatalogBytes)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, n.file.Close(), c.failOwner(key, "close")) }()
		if err = c.reserveOwnerEffect(ctx, c.original.config, n.file, key); err != nil {
			return err
		}
		if err = c.failOwner(key, "write"); err != nil {
			return err
		}
		written, e := n.file.WriteAt(raw, 0)
		if e != nil || written != len(raw) {
			return errors.Join(storagebudget.ErrUnknown, e)
		}
		if err = c.failOwner(key, "sync"); err != nil {
			return err
		}
		if err = n.file.Sync(); err != nil {
			return err
		}
		if err = c.failOwner(key, "readback"); err != nil {
			return err
		}
		b := make([]byte, len(raw))
		if _, err = n.file.ReadAt(b, 0); err != nil || !bytes.Equal(b, raw) {
			return errors.Join(storagebudget.ErrUnknown, err)
		}
		n.immutable = raw
		return n.current()
	})
}

func (c *storageBudgetConstruction) completeOwnerTransaction(ctx context.Context, cat *storageOwnerCatalogV2, bank int, raw []byte) error {
	v, err := c.ref.cell.q.InspectHost(ctx)
	if err != nil {
		return err
	}
	before, after, err := storageOwnerActualDefinitions(v, *cat)
	if err != nil {
		return err
	}
	if cat.Phase == "active" {
		if !after {
			return errStorageOwnerRecovery
		}
		return c.completeCatalogPersistence(ctx, bank, raw, "config/active")
	}
	if !before && !after {
		return errStorageOwnerRecovery
	}
	if before && !after {
		if !v.Configuration.Maintenance || v.Configuration.Revision != cat.ExpectedQRevision {
			return errStorageOwnerRecovery
		}
		if err = c.completeCatalogPersistence(ctx, bank, raw, "config/prepared"); err != nil {
			return err
		}
		if err = c.reserveOwnerEffect(ctx, c.anchor, nil, "anchor/register"); err != nil {
			return err
		}
		b, a, e := cat.definitions()
		if e != nil {
			return e
		}
		add := storageOwnerMissingDefinitions(b, a)
		if err = c.requireConstructionMutation(ctx); err != nil {
			return err
		}
		if err = c.ref.cell.q.Register(ctx, v.Configuration.Revision, add); err != nil {
			return err
		}
		v, err = c.ref.cell.q.InspectHost(ctx)
		if err != nil {
			return err
		}
		_, after, err = storageOwnerActualDefinitions(v, *cat)
		if err != nil || !after {
			return errors.Join(errStorageOwnerRecovery, err)
		}
	}
	if cat.Sequence == ^uint64(0) {
		return storagebudget.ErrBounds
	}
	next := *cat
	next.Sequence++
	next.Phase = "active"
	copy(next.PreviousCatalogDigest[:], raw[32:64])
	frame, err := storageOwnerEncodeCatalogV3(storageOwnerCatalogV3(next), *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
	if err != nil {
		return err
	}
	if err = c.publishOwnerCatalog(ctx, 1-bank, frame, "config/active"); err != nil {
		return err
	}
	*cat = next
	return nil
}

func (c *storageBudgetConstruction) completeCatalogPersistence(ctx context.Context, bank int, raw []byte, key string) error {
	if c.grants[key] {
		return nil
	}
	return c.ownerGate(ctx, func(ctx context.Context) (err error) {
		n, err := storageOpenFile(c.original.config, []string{"storage-owner.a", "storage-owner.b"}[bank], storageOwnerCatalogBytes)
		if err != nil {
			return err
		}
		n.immutable = raw
		defer func() { err = errors.Join(err, n.file.Close()) }()
		return c.completeOwnerFile(ctx, n, key)
	})
}

func storageOwnerMissingDefinitions(before, after storageOwnerDefinitions) storagebudget.Registration {
	r := storagebudget.Registration{}
	ids := map[storagebudget.ID]bool{}
	for _, v := range before.Roots {
		ids[v.ID] = true
	}
	for _, v := range after.Roots {
		if !ids[v.ID] {
			r.Roots = append(r.Roots, storageOwnerRootValue(v))
		}
	}
	ids = map[storagebudget.ID]bool{}
	for _, v := range before.Workspaces {
		ids[v.ID] = true
	}
	for _, v := range after.Workspaces {
		if !ids[v.ID] {
			r.Workspaces = append(r.Workspaces, storagebudget.Workspace{ID: v.ID, Generation: v.Generation, Limits: v.Limits})
		}
	}
	ids = map[storagebudget.ID]bool{}
	for _, v := range before.Filesystems {
		ids[v.ID] = true
	}
	for _, v := range after.Filesystems {
		if !ids[v.ID] {
			r.Filesystems = append(r.Filesystems, v)
		}
	}
	ids = map[storagebudget.ID]bool{}
	for _, v := range before.Constraints {
		ids[v.ID] = true
	}
	for _, v := range after.Constraints {
		if !ids[v.ID] {
			r.CapacityConstraints = append(r.CapacityConstraints, v)
		}
	}
	return r
}

func (c *storageBudgetConstruction) publishOwnerReady(ctx context.Context, cat storageOwnerCatalogV2) error {
	return c.ownerGate(ctx, func(ctx context.Context) (err error) {
		b, n, e := storageOwnerReadFixed(c.original.config, storageReadyName, 32)
		if e != nil {
			if _, le := c.original.config.Lstat(storageReadyName); !errors.Is(le, os.ErrNotExist) {
				return errors.Join(errStorageBootstrap, e)
			}
			v, ve := c.ref.cell.q.InspectHost(ctx)
			if ve != nil {
				return ve
			}
			_, after, ve := storageOwnerActualDefinitions(v, cat)
			if ve != nil || !after || cat.Sequence != 2 || cat.ExpectedQRevision == ^uint64(0) || v.Configuration.Revision != cat.ExpectedQRevision+1 || v.Configuration.Epoch != 1 || !v.Configuration.Maintenance || len(cat.BeforeDefinitionIDs.Roots)+len(cat.BeforeDefinitionIDs.Workspaces)+len(cat.BeforeDefinitionIDs.Filesystems)+len(cat.BeforeDefinitionIDs.Constraints) != 0 {
				return errors.Join(errStorageOwnerRecovery, ve)
			}
			for _, slot := range v.Slots {
				if slot.Ticket.State != storagebudget.Empty {
					return errStorageOwnerRecovery
				}
			}
			n, e = c.createOwnerFile(ctx, c.original.config, storageReadyName, "config/ready", c.ref.cell.binding[:], 32)
			if e != nil {
				return e
			}
			return errors.Join(n.current(), n.file.Close())
		}
		defer func() { err = errors.Join(err, n.file.Close()) }()
		if !bytes.Equal(b, c.ref.cell.binding[:]) {
			return errStorageCustody
		}
		n.immutable = append([]byte(nil), b...)
		return c.completeOwnerFile(ctx, n, "config/ready-complete")
	})
}

func (c *storageBudgetConstruction) acquireStartupHolds(ctx context.Context, cat storageOwnerCatalogV2) error {
	ctx, err := c.constructionMutationContext(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	reused := c.exclusiveCensusEligible || c.exclusiveCensus != nil || c.startupUse != nil || c.startupWork != nil || len(c.startupHolds) != 0
	c.mu.Unlock()
	if reused {
		return storagebudget.ErrAuthority
	}
	var view storagebudget.HostInspection
	err = c.owner.withControl(ctx, func(ctx context.Context, t *storageIntentTable) error {
		var e error
		view, e = c.ref.cell.q.InspectHost(ctx)
		if e != nil {
			return e
		}
		for _, slot := range view.Slots {
			if storageUnretired(slot) {
				return storagebudget.ErrBusy
			}
		}
		for _, slot := range t.slots {
			if !slot.record.reusable() {
				return errStorageOwnerRecovery
			}
		}
		return c.owner.validateIntentMappings(ctx, t, storageHostState(view))
	})
	if err != nil {
		return err
	}
	before, after, err := storageOwnerActualDefinitions(view, cat)
	if err != nil {
		return err
	}
	if !before && !after {
		return errStorageOwnerRecovery
	}
	if before && !after && (!view.Configuration.Maintenance || view.Configuration.Revision != cat.ExpectedQRevision) {
		return errStorageOwnerRecovery
	}
	if !view.Configuration.Maintenance {
		if !after {
			return errStorageOwnerRecovery
		}
		if cat.Phase == "active" {
			if err = c.exclusiveBoundary("before-exclusive-selection"); err != nil {
				return err
			}
			if err = c.selectExclusiveCensus(); err != nil {
				return err
			}
		} else {
			if err = c.reserveOwnerEffect(ctx, c.anchor, nil, "anchor/maintenance"); err != nil {
				return err
			}
			if err = c.requireConstructionMutation(ctx); err != nil {
				return err
			}
			c.startupCensusEpoch, err = c.ref.cell.q.BeginMaintenance(ctx, view.Configuration.Revision)
			if err != nil {
				return err
			}
		}
	}
	use, err := c.owner.maintenanceUse()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.startupUse = use
	c.mu.Unlock()
	work, e := storageOpenOriginalControl(c.ref.cell, ".data-work.lock")
	if e != nil {
		return e
	}
	if e = lockPrivateData(work.file, true); e != nil {
		return errors.Join(storagebudget.ErrBusy, e, work.file.Close())
	}
	c.startupWork = &work
	if e = work.current(); e != nil {
		return e
	}
	for _, r := range c.owner.orderedRoots() {
		h, e := c.owner.domain(ctx, r, c.startupUse)
		if e != nil {
			return e
		}
		c.startupHolds = append(c.startupHolds, h)
	}
	return nil
}

func (c *storageBudgetConstruction) validateStartupHostJournal(ctx context.Context, r storageOwnerRecordV2, requireEmpty bool) error {
	return c.ownerGate(ctx, func(ctx context.Context) (err error) {
		raw, n, err := storageOwnerReadFixed(c.original.config, storageIntentName, storageIntentBytes)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, n.file.Close()) }()
		for start := 0; start < len(raw); start += storageIntentPage {
			if bytes.Equal(raw[start:start+8], []byte("DSKHINT1")) {
				return storagebudget.ErrUnsupported
			}
		}
		for i := 0; i < storagebudget.Slots; i++ {
			if err = ctx.Err(); err != nil {
				return err
			}
			start := i * 2 * storageIntentPage
			selected, e := storageSelectCreationHost(raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage], i, r.Installation, r.QBinding)
			s := selected.Slot
			if e != nil {
				return e
			}
			if selected.Creation != nil && selected.Creation.Kind == storageCreationLocalPrivate && !requireEmpty {
				c.startupLocal = true
				continue
			}
			if !s.record.reusable() || requireEmpty && s.record.Phase != storageIntentEmpty {
				return errStorageOwnerRecovery
			}
		}
		return n.current()
	})
}
func (c *storageBudgetConstruction) releaseStartupHolds() (err error) {
	c.mu.Lock()
	p := c.exclusiveCensus
	c.mu.Unlock()
	if p != nil {
		p.mu.Lock()
		busy := p.running || p.helper != nil || p.borrowers != 0
		p.mu.Unlock()
		if busy {
			return storagebudget.ErrBusy
		}
	}
	for i := len(c.startupHolds) - 1; i >= 0; i-- {
		err = errors.Join(err, c.startupHolds[i].Close())
	}
	c.startupHolds = nil
	if c.startupWork != nil {
		err = errors.Join(err, c.startupWork.file.Close())
		c.startupWork = nil
	}
	if c.startupUse != nil {
		err = errors.Join(err, c.startupUse.Close())
		c.startupUse = nil
	}
	return err
}

// Complete current exact valid Q bytes, never InitializeHeld again. Six fixed
// file Sync/Close pairs plus the anchor directory are one finite persistence
// group. The complete InspectHost tuple must be unchanged after all closes.
func (c *storageBudgetConstruction) completeQPersistence(ctx context.Context) error {
	return c.ownerGate(ctx, func(ctx context.Context) error {
		before, err := c.ref.cell.q.InspectHost(ctx)
		if err != nil {
			return err
		}
		if err = c.reserveOwnerEffect(ctx, c.anchor, nil, "anchor/complete-state"); err != nil {
			return err
		}
		parent, err := c.anchor.Open(".")
		if err != nil {
			return err
		}
		for _, name := range []string{"lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"} {
			if err = ctx.Err(); err != nil {
				break
			}
			n, e := c.openOriginalAnchorControl(ctx, name)
			if e != nil {
				err = e
				break
			}
			facts, e := storageAdvisoryReadExistingLeaseHeld(ctx, parent, n.file)
			if e == nil {
				p := storageInfrastructureStart{spent: c.spent}
				e = p.checkAvailability(facts, storagebudget.CapacityVector{})
			}
			if e == nil {
				e = c.failOwner("anchor/complete-state/"+name, "sync")
			}
			if e == nil {
				e = n.file.Sync()
			}
			e = errors.Join(e, n.current(), n.file.Close(), c.failOwner("anchor/complete-state/"+name, "close"))
			if e != nil {
				err = e
				break
			}
		}
		if err == nil {
			err = c.failOwner("anchor/complete-state", "parent-sync")
		}
		if err == nil {
			err = parent.Sync()
		}
		err = errors.Join(err, parent.Close(), c.failOwner("anchor/complete-state", "parent-close"))
		if err != nil {
			return err
		}
		if err = c.failOwner("anchor/complete-state", "readback"); err != nil {
			return err
		}
		after, err := c.ref.cell.q.InspectHost(ctx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			return errStorageOwnerRecovery
		}
		return nil
	})
}

func storageOwnerAnchorState(root *os.Root) (onlyLock bool, err error) {
	f, err := root.Open(".")
	if err != nil {
		return false, err
	}
	names, re := f.ReadDir(7)
	err = errors.Join(re, f.Close())
	if err != nil {
		return false, err
	}
	if len(names) == 1 && names[0].Name() == "lock" {
		return true, nil
	}
	if len(names) != 6 {
		return false, errStorageBootstrap
	}
	wanted := map[string]bool{"lock": true, "checkpoint.a": true, "checkpoint.b": true, "tickets.a": true, "tickets.b": true, "manifests": true}
	for _, n := range names {
		if !wanted[n.Name()] {
			return false, errStorageBootstrap
		}
		delete(wanted, n.Name())
	}
	if len(wanted) != 0 {
		return false, errStorageBootstrap
	}
	return false, nil
}
func (c *storageBudgetConstruction) requireBlankCatalogs(ctx context.Context) error {
	for _, name := range []string{"storage-owner.a", "storage-owner.b"} {
		b, n, err := storageOwnerReadFixed(c.original.config, name, storageOwnerCatalogBytes)
		if err != nil {
			return err
		}
		if !storageAllZero(b) {
			err = errStorageBootstrap
		}
		err = errors.Join(err, n.current(), n.file.Close(), ctx.Err())
		if err != nil {
			return err
		}
	}
	return nil
}

// A catalog must subsequently bind all three native objects before any transfer
// admission. Merely finding these names does not authorize a new feature.
func (c *storageBudgetConstruction) retainWholeTransferControls(ctx context.Context, fresh bool) error {
	rows := []struct {
		name, key string
		size      int64
	}{
		{".data-transfer.lock", "config/transfer-lock", 0},
		{storageTransferBankA, "config/transfer-a-create", storageTransferBankBytes},
		{storageTransferBankB, "config/transfer-b-create", storageTransferBankBytes},
	}
	present := 0
	for _, row := range rows {
		_, err := c.original.config.Lstat(row.name)
		if err == nil {
			present++
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !fresh && present == 0 {
		return nil
	}
	if !fresh && present != len(rows) {
		return errStorageBootstrap
	}
	for _, row := range rows {
		n, err := storageOpenFile(c.original.config, row.name, row.size)
		if err != nil {
			if _, le := c.original.config.Lstat(row.name); !fresh || !errors.Is(le, os.ErrNotExist) {
				return errors.Join(errStorageBootstrap, err)
			}
			n, err = c.createOwnerFile(ctx, c.original.config, row.name, row.key, nil, row.size)
			if err != nil {
				return err
			}
		}
		c.original.files = append(c.original.files, n)
	}
	return nil
}
