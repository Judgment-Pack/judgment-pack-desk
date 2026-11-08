package desk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Reach the ordinary configured-history issuer after actual project enrollment.
// The project domain is a legitimate borrowed hold with no owned File; it is
// unrelated to the configured parent, which still needs its own creator EX.
func TestStorageCreationHistoryAfterProjectEnrollmentKeepsBorrowedDomain(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	c := s.storageConstruction
	ctx := t.Context()
	storageTestMust(t, c.enrollOriginalProjectParent(ctx))
	if c.ref.cell.enrollmentEndpoint == nil {
		t.Fatal("actual project enrollment did not install its endpoint")
	}
	p, err := c.beginEnrollmentEpoch(ctx)
	storageTestMust(t, err)
	defer func() {
		if err := p.finish(nil); err != nil {
			t.Error(err)
		}
	}()
	storageTestMust(t, p.acquire(ctx))
	var borrowed *storageDomainHold
	for _, hold := range p.holds {
		if !hold.root.domainDirectory || hold.borrowed == nil {
			continue
		}
		actual := hold
		for actual.borrowed != nil {
			actual = actual.borrowed
		}
		if actual.file == p.project {
			if hold.file != nil {
				t.Fatal("project alias unexpectedly owns a second description")
			}
			storageTestMust(t, hold.current(ctx))
			borrowed = hold
		}
	}
	if borrowed == nil {
		t.Fatal("next actual epoch did not borrow its retained project EX")
	}
	storageTestMust(t, p.captureExistingOriginal(ctx))
	storageTestMust(t, p.qualifyOriginalNative(ctx))
	proposed, payloads, err := p.planDemandedHistory(ctx, s)
	storageTestMust(t, err)
	if proposed == nil || proposed.Kind != storageCreationHistory || proposed.Branch != storageCreationConfigured || proposed.SelectedPath != filepath.Join(cfg.DeskConfigDir, "data") {
		t.Fatal("actual fixture did not demand configured history creation")
	}
	member, err := p.issueOriginalCreationFor(ctx, s, *proposed, payloads)
	storageTestMust(t, err)
	storageTestMust(t, member.current(ctx))
	if len(member.parents) != 2 {
		t.Fatal("configured creation lost its History and Control parents")
	}
	first, second := member.parents[0], member.parents[1]
	if first.project != nil || first.domain != nil || first.lease == nil || first.borrow == nil || first.file == p.project || second.lease != first.lease || second.borrow != first.borrow || second.file != first.file {
		t.Fatal("configured parent did not retain one actual independent creator lease")
	}
	competing, err := p.originalProjectRoot().Open(".")
	storageTestMust(t, err)
	defer competing.Close()
	if err := lockProjectFile(competing); !lockHeld(err) {
		t.Fatal("creation admission lost the original project EX", err)
	}
	storageTestMust(t, member.Close())
	if err := member.current(ctx); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("closed creation retained parent authority", err)
	}
	if _, err := p.project.Stat(); err != nil {
		t.Fatal("creation Close closed its borrowed project owner", err)
	}
	storageTestMust(t, borrowed.current(ctx))
	if err := lockProjectFile(competing); !lockHeld(err) {
		t.Fatal("creation Close released borrowed project EX", err)
	}
	// Closing the alias invalidates its authority without closing the owner.
	storageTestMust(t, borrowed.Close())
	if err := p.held(ctx); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("closed domain alias retained epoch authority", err)
	}
	if _, err := p.project.Stat(); err != nil {
		t.Fatal("alias Close closed the original project description", err)
	}
	if err := lockProjectFile(competing); !lockHeld(err) {
		t.Fatal("alias Close released its original project EX", err)
	}
	for _, name := range []string{"data", dataLocationName} {
		if _, err := c.original.config.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("creation issuance changed an unexecuted namespace", name, err)
		}
	}
	storageTestMust(t, p.finish(nil))
	storageTestMust(t, lockProjectFile(competing))
}
