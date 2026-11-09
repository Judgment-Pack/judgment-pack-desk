package desk

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

type storageCensusControlParent uint8

const (
	storageCensusUncovered storageCensusControlParent = iota
	storageCensusConfigParent
	storageCensusQParent
)

// Coverage belongs to the original parent, never to an equal name found under
// another root. The currentness checks retain the actual original directory;
// these comparisons do not construct custody from an incoming identity.
func (r *storageRootBinding) censusControlParent() (storageCensusControlParent, error) {
	if r.configControl && r.qControl {
		return storageCensusUncovered, storagebudget.ErrAuthority
	}
	if !r.configControl && !r.qControl {
		return storageCensusUncovered, nil
	}
	if r.owner == nil || !r.owner.valid() {
		return storageCensusUncovered, storagebudget.ErrAuthority
	}
	if r.configControl {
		if r.directory != r.owner.ref.cell.record.Config {
			return storageCensusUncovered, storagebudget.ErrAuthority
		}
		return storageCensusConfigParent, nil
	}
	if r.directory != r.owner.ref.cell.record.Anchor {
		return storageCensusUncovered, storagebudget.ErrAuthority
	}
	return storageCensusQParent, nil
}

// This is the complete literal coverage partition of the unchanged provision.
// Ready is measured separately; catalog-a/b and the optional work lock are not
// covered by name. The one selected original domain is joined separately to
// its retained witness. Nested names, unknown controls and other parents pay
// their actual byte/namespace charge through the normal census/dedup path.
func (parent storageCensusControlParent) covers(relative string) bool {
	if strings.Contains(relative, "/") {
		return false
	}
	switch parent {
	case storageCensusQParent:
		switch relative {
		case "lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests":
			return true
		}
	case storageCensusConfigParent:
		switch relative {
		case ".home-use.lock", storageBootstrapName, storageUseName, storageClaimName, storageRecordName, storageIntentName, projectMoveName:
			return true
		}
	}
	return false
}

func (r *storageRootBinding) censusCovered(ctx context.Context, relative string, info os.FileInfo) (bool, error) {
	parent, e := r.censusControlParent()
	if e != nil {
		return false, e
	}
	if strings.Contains(relative, "/") {
		return false, nil
	}
	if parent == storageCensusConfigParent && relative == projectMoveName {
		// Only the selected original config parent may use this fixed control
		// allowance. A filename alone cannot exempt arbitrary/malformed bytes.
		if !storageCensusPrivateFile(info) || info.Size() <= 0 || info.Size() > 16<<10 || !storageMoveControlProvisionFits() {
			return false, storagebudget.ErrBounds
		}
		raw, _, e := storageMoveReadPrivate(ctx, r.root, relative, 16<<10)
		if e != nil {
			return false, e
		}
		intent, found, e := decodeProjectMove(raw)
		if e != nil || !found {
			return false, errors.Join(errStorageCustody, e)
		}
		if intent.Storage != nil && (intent.Storage.Installation != r.owner.ref.cell.record.Installation || intent.Storage.Binding != r.owner.ref.cell.binding) {
			return false, errStorageCustody
		}
		return true, nil
	}
	if parent == storageCensusConfigParent && relative == r.domainName {
		// The existing original constructor can select another literal domain
		// name. Its actual retained parent/name/inode, not the string alone,
		// supplies this one additional coverage row.
		n := r.domain
		if n.file == nil {
			return false, nil
		}
		if n.root != r.root || n.name != relative || n.size != 0 || !os.SameFile(info, n.identity) {
			return false, storagebudget.ErrAuthority
		}
		if e = n.current(); e != nil {
			return false, e
		}
		return true, nil
	}
	return parent.covers(relative), nil
}

func (parent storageCensusControlParent) provision(readyMeasured bool) storagebudget.Vector {
	switch parent {
	case storageCensusQParent:
		return storagebudget.Vector{Bytes: storagebudget.MetadataBudget, Files: 16, Directories: 1}
	case storageCensusConfigParent:
		v := storagebudget.Vector{Bytes: storageHostBudget, Files: 16, Directories: 4}
		if readyMeasured {
			v.Bytes -= 32
			v.Files--
		}
		return v
	}
	return storagebudget.Vector{}
}

// Census Ready is an exact lexical content/namespace observation. It owns one
// fresh normal description only for this measurement and never records a
// durable Ready inode or promotes it into an installation identity.
func storageCensusReady(root *os.Root, binding storagebudget.Digest, before os.FileInfo, charge func(storageDirectoryIdentity) error, observe ...func(os.FileInfo, os.FileInfo)) (err error) {
	if !storagePrivate(before, false) || before.Size() != 32 {
		return storagebudget.ErrUnknown
	}
	f, e := root.OpenFile(storageReadyName, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if e != nil {
		return e
	}
	n := storageNamedFile{root: root, name: storageReadyName, file: f, identity: before, size: 32, immutable: binding[:]}
	// All paths after open join the one owned Close, including charge errors.
	defer func() {
		ce := n.file.Close()
		after, ae := root.Lstat(storageReadyName)
		if ae != nil || !storagePrivate(after, false) || !os.SameFile(before, after) || after.Size() != before.Size() || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
			err = errors.Join(err, storagebudget.ErrUnknown)
		}
		err = errors.Join(err, ce)
		if err == nil && len(observe) == 1 && observe[0] != nil {
			observe[0](before, after)
		}
	}()
	if e = n.current(); e != nil {
		return e
	}
	id, e := storageFileIdentity(n.identity)
	if e != nil {
		return e
	}
	if e = charge(id); e != nil {
		return e
	}
	return n.current()
}

// The complete literal config-floor partition: three zero-byte locks, the
// selected zero-byte data domain, two original <=4096 records, fixed2MiB J,
// and one strict <=16KiB move intent. Ready, catalogs, packet/evidence controls,
// transfer banks, work lock and EVERY staging/temporary name remain separately
// charged. claimPrivateData creates the intent directly with O_EXCL (no stage).
// Four provisioned directories and all original limits remain unchanged.
func storageMoveControlProvisionFits() bool {
	const coveredBytes = storageIntentBytes + 2*storageRecordLimit + (16 << 10)
	const coveredFiles = 3 + 1 + 2 + 1 + 1
	provision := storageCensusConfigParent.provision(false)
	return coveredBytes <= provision.Bytes && coveredFiles <= provision.Files && provision.Directories == 4
}
