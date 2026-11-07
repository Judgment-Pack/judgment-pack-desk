package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStoragePreparationPrefixDescriptionBorrowAndBusyClose(t *testing.T) {
	cfg := ownerActualConfig(t)
	// This comparison-only alias uses the private directory identity helper.
	// TempDir permissions depend on the toolchain and umask; provision its
	// required custody explicitly before opening the actual original project.
	storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
	_, c, _ := ownerCrashConstruction(t, cfg)
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	p, err := c.beginStorageIdentityPreparation(t.Context())
	storageTestMust(t, err)
	defer p.Close()
	l, err := p.lockProjectPrefix(t.Context())
	storageTestMust(t, err)
	// This comparison-only fixture names the actual original project and actual
	// held directory descriptions. Its test-only alias row is never registered
	// in Q or published in a catalog; it does not prove a selected identity route.
	witness, err := c.server.root.Open(".")
	storageTestMust(t, err)
	defer witness.Close()
	info, err := witness.Stat()
	storageTestMust(t, err)
	dir, err := storageDirectoryID(c.server.root)
	storageTestMust(t, err)
	r := &storageRootBinding{owner: c.owner, root: c.server.root, directory: dir, domainDirectory: true, domainName: ".", domain: storageNamedFile{root: c.server.root, name: ".", file: witness, identity: info}}
	r.self = r
	r.row.ID = storageIntentTestID(246)
	if c.owner.roots[r.row.ID] != nil {
		t.Fatal("test alias ID collides with actual catalog")
	}
	c.owner.roots[r.row.ID] = r
	defer delete(c.owner.roots, r.row.ID)
	use, err := c.owner.maintenanceUse()
	storageTestMust(t, err)
	c.startupUse = use
	defer c.releaseStartupHolds()
	d, err := p.domain(t.Context(), r, use)
	storageTestMust(t, err)
	defer d.Close()
	duplicate, err := p.domain(t.Context(), r, use)
	storageTestMust(t, err)
	defer duplicate.Close()
	if d.file != nil || d.prefix == nil || d.prefix.lease != l || duplicate.borrowed != d {
		t.Fatal("alias acquired/owned another description instead of borrowing exact prefix")
	}
	probe, err := c.server.root.Open(".")
	storageTestMust(t, err)
	defer probe.Close()
	if err := lockProjectFile(probe); err == nil {
		t.Fatal("prefix did not hold actual project EX")
	}
	copied := &storagePrefixLease{preparation: l.preparation, owner: l.owner, generation: l.generation, root: l.root, file: l.file, info: l.info, persistent: l.persistent, signing: l.signing, borrowers: l.borrowers, borrows: l.borrows, closing: l.closing, closed: l.closed, closeErr: l.closeErr}
	copied.self = copied
	if err := copied.Close(); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("copied prefix owned original Close", err)
	}
	if err := p.Close(); !errors.Is(err, storagebudget.ErrBusy) {
		t.Fatal("preparation released a live domain borrower", err)
	}
	if _, err := l.file.Stat(); err != nil {
		t.Fatal("Busy Close discarded original locked description", err)
	}
	if err := c.beginAdmissionHandoff(t.Context()); err == nil {
		t.Fatal("live prefix/preparation yielded handoff")
	}
	storageTestMust(t, duplicate.Close())
	storageTestMust(t, d.Close())
	if _, err := l.file.Stat(); err != nil {
		t.Fatal("borrower Close released original prefix", err)
	}
	if err := lockProjectFile(probe); err == nil {
		t.Fatal("borrower Close released project EX")
	}
	storageTestMust(t, p.Close())
	storageTestMust(t, p.Close())
	if _, err := l.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("final drain did not close exact original prefix", err)
	}
	if err := lockProjectFile(probe); err != nil {
		t.Fatal("final drain retained project EX", err)
	}
	storageTestMust(t, c.releaseStartupHolds())
	delete(c.owner.roots, r.row.ID)
	storageTestMust(t, c.beginAdmissionHandoff(t.Context()))
	if err := p.current(t.Context()); err == nil {
		t.Fatal("invalidated preparation survived handoff")
	}
}

func TestStoragePreparationPrefixErrorsAndNamedReplacement(t *testing.T) {
	for _, cut := range []string{"actual-close-error", "signing-name", "foreign-domain"} {
		t.Run(cut, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.MkdirAll(filepath.Join(cfg.DeskConfigDir, secretsDirName, signingDirName), 0700))
			_, c, _ := ownerCrashConstruction(t, cfg)
			storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
			p, err := c.beginStorageIdentityPreparation(t.Context())
			storageTestMust(t, err)
			defer p.Close()
			l, err := p.lockProjectPrefix(t.Context())
			storageTestMust(t, err)
			switch cut {
			case "actual-close-error":
				storageTestMust(t, l.file.Close()) // real descriptor error, no fake close hook
				first := l.Close()
				if !errors.Is(first, os.ErrClosed) || l.Close() != first {
					t.Fatal("actual Close error hidden, retried, or changed", first)
				}
				if err := p.Close(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("preparation lost prefix Close error", err)
				}
				if err := c.beginAdmissionHandoff(t.Context()); err == nil {
					t.Fatal("failed prefix Close yielded handoff")
				}
			case "signing-name":
				signing, err := p.lockSigningPrefix(t.Context())
				storageTestMust(t, err)
				name := filepath.Join(cfg.DeskConfigDir, secretsDirName, signingDirName)
				storageTestMust(t, os.Rename(name, name+".original"))
				storageTestMust(t, os.Mkdir(name, 0700))
				if err := signing.current(t.Context()); err == nil {
					t.Fatal("replacement signing name inherited original prefix")
				}
				if err := p.Close(); err == nil {
					t.Fatal("prefix currentness error was lost during Close")
				}
				if err := c.beginAdmissionHandoff(t.Context()); err == nil {
					t.Fatal("named replacement yielded handoff")
				}
			case "foreign-domain":
				other, r, _ := storageWriterFixture(t, false)
				_ = other
				if b, err := l.borrow(t.Context(), r); b != nil || !errors.Is(err, storagebudget.ErrAuthority) {
					t.Fatal("foreign/non-alias domain borrowed original prefix", err)
				}
			}
		})
	}
}

func TestStoragePreparationTwoPrefixesSigningBorrowRetainsProject(t *testing.T) {
	for _, priorError := range []bool{false, true} {
		name := "clean"
		if priorError {
			name = "accumulated-error"
		}
		t.Run(name, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.MkdirAll(filepath.Join(cfg.DeskConfigDir, secretsDirName, signingDirName), 0700))
			_, c, _ := ownerCrashConstruction(t, cfg)
			storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
			p, err := c.beginStorageIdentityPreparation(t.Context())
			storageTestMust(t, err)
			defer p.Close()
			project, err := p.lockProjectPrefix(t.Context())
			storageTestMust(t, err)
			signing, err := p.lockSigningPrefix(t.Context())
			storageTestMust(t, err)
			projectFD, signingFD := project.file, signing.file

			// The alias is comparison-only: actual original construction and held
			// descriptions, with no Q/catalog publication or identity producer.
			witness, err := signing.root.Open(".")
			storageTestMust(t, err)
			defer witness.Close()
			info, err := witness.Stat()
			storageTestMust(t, err)
			directory, err := storageDirectoryID(signing.root)
			storageTestMust(t, err)
			r := &storageRootBinding{owner: c.owner, root: signing.root, directory: directory, domainDirectory: true, domainName: ".", domain: storageNamedFile{root: signing.root, name: ".", file: witness, identity: info}}
			r.self = r
			r.row.ID = storageIntentTestID(247)
			if c.owner.roots[r.row.ID] != nil {
				t.Fatal("test signing alias ID collides with actual catalog")
			}
			c.owner.roots[r.row.ID] = r
			defer delete(c.owner.roots, r.row.ID)
			use, err := c.owner.maintenanceUse()
			storageTestMust(t, err)
			c.startupUse = use
			defer c.releaseStartupHolds()
			domain, err := p.domain(t.Context(), r, use)
			storageTestMust(t, err)
			defer domain.Close()
			if domain.file != nil || domain.prefix == nil || domain.prefix.lease != signing {
				t.Fatal("signing domain did not borrow its exact prefix description")
			}

			projectProbe, err := project.root.Open(".")
			storageTestMust(t, err)
			defer projectProbe.Close()
			signingProbe, err := signing.root.Open(".")
			storageTestMust(t, err)
			defer signingProbe.Close()
			projectContender, err := project.root.Open(".")
			storageTestMust(t, err)
			defer projectContender.Close()
			signingContender, err := signing.root.Open(".")
			storageTestMust(t, err)
			defer signingContender.Close()
			prefixes := []struct {
				name             string
				lease            *storagePrefixLease
				original         *os.File
				info             os.FileInfo
				probe, contender *os.File
				lock             func(*os.File) error
			}{
				{"project", project, projectFD, project.info, projectProbe, projectContender, lockProjectFile},
				{"signing", signing, signingFD, signing.info, signingProbe, signingContender, lockSigningFile},
			}
			c.owner.mu.Lock()
			active := c.owner.active
			c.owner.mu.Unlock()
			if active < 2 {
				t.Fatal("fixture lacks startup and preparation owner retains", active)
			}
			assertHeld := func(signingBorrowers uint64) {
				t.Helper()
				g := &c.owner.admission
				g.mu.Lock()
				count, shared, preparation := g.prefixes, g.shared, g.preparation
				g.mu.Unlock()
				if count != 2 || shared != 0 || preparation != p {
					t.Fatal("Busy/drain changed counted prefixes or preparation", count, shared)
				}
				c.owner.mu.Lock()
				retains := c.owner.active
				c.owner.mu.Unlock()
				if retains != active {
					t.Fatal("Busy/drain dropped the preparation owner retain", retains, active)
				}
				for _, x := range prefixes {
					if x.lease.file != x.original {
						t.Fatal("prefix description replaced", x.name)
					}
					held, err := x.original.Stat()
					if err != nil || !os.SameFile(held, x.info) {
						t.Fatal("Busy/drain lost original prefix description", x.name, err)
					}
					probe, err := x.probe.Stat()
					if err != nil || !os.SameFile(probe, x.info) {
						t.Fatal("competing description does not name original prefix", x.name, err)
					}
					// These competing descriptions check continuous EX; aliasing was
					// already established by the sealed original/native borrow above.
					if err := x.lock(x.probe); !lockHeld(err) {
						t.Fatal("Busy/drain released an earlier or later prefix EX", x.name, err)
					}
					x.lease.mu.Lock()
					borrowers, registered, closed := x.lease.borrowers, len(x.lease.borrows), x.lease.closed
					x.lease.mu.Unlock()
					want := uint64(0)
					if x.lease == signing {
						want = signingBorrowers
					}
					if borrowers != want || uint64(registered) != want || closed {
						t.Fatal("Busy/drain changed live borrower ownership", x.name, borrowers, registered, closed)
					}
				}
			}
			assertHeld(1)
			var accumulated error
			if priorError {
				// A real duplicate-acquisition refusal supplies a previously
				// accumulated error without closing either original description.
				_, accumulated = p.lockSigningPrefix(t.Context())
				if !errors.Is(accumulated, storagebudget.ErrAuthority) {
					t.Fatal("duplicate signing prefix unexpectedly acquired", accumulated)
				}
			}
			for i := 0; i < 2; i++ {
				err := p.Close()
				if !errors.Is(err, storagebudget.ErrBusy) || priorError && !errors.Is(err, storagebudget.ErrAuthority) {
					t.Fatal("Busy Close lost borrower or accumulated error", err)
				}
				assertHeld(1)
			}
			storageTestMust(t, domain.Close())
			storageTestMust(t, domain.Close())
			assertHeld(0) // Borrower drain alone closes neither prefix.
			closed := p.Close()
			if priorError {
				if !errors.Is(closed, storagebudget.ErrAuthority) {
					t.Fatal("final cleanup lost accumulated refusal", closed)
				}
			} else {
				storageTestMust(t, closed)
			}
			if p.Close() != closed {
				t.Fatal("repeated final Close changed recorded result")
			}
			g := &c.owner.admission
			g.mu.Lock()
			count, preparation := g.prefixes, g.preparation
			g.mu.Unlock()
			if count != 0 || preparation != nil {
				t.Fatal("final Close retained prefix counts or preparation", count)
			}
			for _, x := range prefixes {
				if x.lease.file != x.original {
					t.Fatal("final Close substituted another description", x.name)
				}
				if _, err := x.original.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("final Close did not close original prefix", x.name, err)
				}
				if err := x.lock(x.probe); err != nil {
					t.Fatal("final Close retained original EX", x.name, err)
				}
			}
			if p.Close() != closed {
				t.Fatal("repeated Close changed result after other descriptions acquired EX")
			}
			for _, x := range prefixes {
				info, err := x.probe.Stat()
				if err != nil || !os.SameFile(info, x.info) {
					t.Fatal("repeated Close affected an independently owned description", x.name, err)
				}
				if err := x.lock(x.contender); !lockHeld(err) {
					t.Fatal("repeated Close released an independently owned EX", x.name, err)
				}
			}
			g.mu.Lock()
			count, shared, preparation := g.prefixes, g.shared, g.preparation
			g.mu.Unlock()
			if count != 0 || shared != 0 || preparation != nil {
				t.Fatal("repeated Close changed final exclusion counts", count, shared)
			}
			for _, x := range prefixes {
				x.lease.mu.Lock()
				borrowers, registered, isClosed := x.lease.borrowers, len(x.lease.borrows), x.lease.closed
				x.lease.mu.Unlock()
				if borrowers != 0 || registered != 0 || !isClosed {
					t.Fatal("final/repeated Close retained borrower ownership", x.name, borrowers, registered, isClosed)
				}
			}
			c.owner.mu.Lock()
			remaining := c.owner.active
			c.owner.mu.Unlock()
			wantActive := active - 1 // preparation retain only on clean cleanup
			if priorError {
				wantActive-- // accumulated error also invalidates the startup session
			}
			if remaining != wantActive {
				t.Fatal("final/repeated Close retained or double-dropped owner references", remaining, wantActive)
			}
		})
	}
}

func TestStorageIdentityOriginalProducerCaptureAndUnqualifiedRefusal(t *testing.T) {
	cfg := ownerActualConfig(t)
	storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
	storageTestMust(t, os.Mkdir(filepath.Join(cfg.ProjectDir, startupIdentityDir), 0700))
	_, c, _ := ownerCrashConstruction(t, cfg)
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	before, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	p, err := c.beginStartupEnrollment(t.Context())
	storageTestMust(t, err)
	defer func() {
		if err := p.finish(nil); err != nil {
			t.Error(err)
		}
	}()
	storageTestMust(t, p.acquireIdentity(t.Context()))
	storageTestMust(t, p.captureInitialOriginal(t.Context()))
	storageTestMust(t, p.captureOriginalIdentityPlan(t.Context()))
	if p.identityPlan == nil || p.identityInitial != (storageCompositionInitial{Mode: 1}) || p.identityOriginal != nil {
		t.Fatal("positive original absence did not select exact Mode1")
	}
	actual, err := parseIdentity(p.identityPlan.IdentityInitial)
	storageTestMust(t, err)
	if actual.ID == digestOf([]byte(c.server.projectDir)) || actual.Folder != c.server.folderKey() || actual.Path != c.server.projectDir {
		t.Fatal("producer replaced legitimate random/folder choice with accounting key")
	}
	if string(p.identityPlan.AccountingOwnerKey) != digestOf([]byte(c.server.projectDir)) {
		t.Fatal("new identity replaced immutable original accounting key")
	}
	if len(p.identityApplications) != 1 || len(p.identityApplications[storageIdentityIntentCreate]) == 0 {
		t.Fatal("producer omitted the original application debt board")
	}
	if err = p.captureOriginalIdentityPlan(t.Context()); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("producer redrew an already retained choice", err)
	}
	// Installed native authority is deliberately absent. This exercises the
	// actual producer and refusal boundary, not native publication success.
	if _, err = p.issueOriginalIdentity(t.Context(), storageIdentityIntentCreate); !errors.Is(err, storagebudget.ErrAuthority) {
		t.Fatal("unadmitted producer issued live member", err)
	}
	if err = p.qualifyOriginalNative(t.Context()); !errors.Is(err, errStorageEnrollmentQualification) {
		t.Fatal("empty installed native evidence unexpectedly qualified", err)
	}
	if p.identityLaunch != nil || p.identity != nil || p.qualified != nil {
		t.Fatal("refusal installed action authority")
	}
	if _, err = os.Lstat(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only producer/refusal published identity", err)
	}
	after, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("producer/refusal changed original Q")
	}
	storageTestMust(t, p.finish(nil))
}

func TestStorageIdentityActualStartupCallerUnqualifiedPreservesOriginal(t *testing.T) {
	if storageEnrollmentInstalledNativeRecords != "[]" {
		t.Fatal("this negative fixture requires the pinned empty installed records")
	}
	for _, mode := range []string{"unseeded", "legacy-no-from", "legacy-from"} {
		t.Run(mode, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
			private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
			storageTestMust(t, os.Mkdir(private, 0700))
			var original []byte
			if mode != "unseeded" {
				info, e := os.Stat(cfg.ProjectDir)
				storageTestMust(t, e)
				record := identityRecord{ID: strings.Repeat("9", 64), Path: cfg.ProjectDir, Folder: identityKey(info)}
				if mode == "legacy-from" {
					record.From = digestOf([]byte(cfg.ProjectDir))
				}
				original = record.line()
				privateFixture(t, filepath.Join(private, startupIdentityName), string(original))
			}
			s, err := New(cfg)
			storageTestMust(t, err)
			defer func() { storageTestMust(t, s.Close()) }()
			if !s.storageReadOnly || !errors.Is(s.storageProblem, errStorageEnrollmentQualification) || s.storageEnrollment != nil {
				t.Fatal("actual startup bypassed original native qualification", s.storageProblem)
			}
			entries, e := os.ReadDir(private)
			storageTestMust(t, e)
			if mode == "unseeded" {
				if len(entries) != 0 {
					t.Fatal("unseeded startup wrote before qualification")
				}
			} else {
				raw, e := os.ReadFile(filepath.Join(private, startupIdentityName))
				storageTestMust(t, e)
				if len(entries) != 1 || !bytes.Equal(raw, original) {
					t.Fatal("pre-lineage identity or From changed before qualification")
				}
			}
			for _, name := range []string{"storage-owner.a", "storage-owner.b"} {
				raw, e := os.ReadFile(filepath.Join(cfg.DeskConfigDir, name))
				storageTestMust(t, e)
				if string(raw[:8]) != "DSKOWN3\x00" {
					t.Fatal("first-base publication preceded qualification")
				}
			}
			q, e := s.storageConstruction.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			for _, slot := range q.View.Slots {
				if slot.Ticket.State != storagebudget.Empty {
					t.Fatal("startup issued Q action before qualification")
				}
			}
		})
	}
}
func TestStorageIdentityLocalProducerUsesOriginalPrivateAndNoEmptyAction(t *testing.T) {
	for _, mode := range []string{"absent-draft", "existing-draft", "complete"} {
		t.Run(mode, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
			private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
			storageTestMust(t, os.Mkdir(private, 0700))
			if mode != "absent-draft" {
				storageTestMust(t, os.Mkdir(filepath.Join(private, "dialect-drafts"), 0700))
			}
			if mode == "complete" {
				privateFixture(t, filepath.Join(private, "dialect-drafts", ".gitignore"), "retained custom ignore\n")
				privateFixture(t, filepath.Join(private, "dialect-drafts", "workflow.lock"), "")
			}
			s, c, _ := ownerCrashConstruction(t, cfg)
			storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
			wrapper, e := c.forServer(s)
			storageTestMust(t, e)
			s.storageEnrollment = wrapper
			defer wrapper.Close()
			before, e := c.ref.cell.q.InspectEnrollment(t.Context())
			storageTestMust(t, e)
			p, e := c.beginStartupEnrollment(t.Context())
			storageTestMust(t, e)
			// Registered after wrapper.Close, so fatal assertions still drain the
			// synchronous epoch before its borrowed wrapper and construction.
			defer func() {
				if err := p.finish(nil); err != nil {
					t.Error(err)
				}
			}()
			storageTestMust(t, p.selectOriginalRequester(t.Context(), s))
			storageTestMust(t, p.acquire(t.Context()))
			storageTestMust(t, p.captureInitialOriginal(t.Context()))
			proposed, payloads, e := p.planDemandedDraft(t.Context(), s)
			storageTestMust(t, e)
			if mode == "complete" {
				if proposed != nil || len(payloads) != 0 {
					t.Fatal("complete descendants produced an action or empty Register")
				}
			} else {
				if proposed == nil || proposed.Kind != storageCreationLocalPrivate || !reflect.DeepEqual(proposed.Tail, storageCreationTail{}) {
					t.Fatal("local producer borrowed a J4 child tail")
				}
				if proposed.Parents[0].Role != 2 || proposed.Core.Target != "dialect-drafts" || proposed.SelectedPath != filepath.Join(private, "dialect-drafts") {
					t.Fatal("local producer lost exact original private path/role")
				}
				want := storagebudget.Vector{Bytes: 2, Files: 2}
				if mode == "absent-draft" {
					want.Directories = 1
					if len(proposed.Parents) != 1 || len(proposed.Effects) != 3 {
						t.Fatal("absent draft closed effects")
					}
				} else {
					if len(proposed.Parents) != 2 || proposed.Parents[1].Role != 3 || len(proposed.Effects) != 2 {
						t.Fatal("existing draft closed effects")
					}
				}
				if proposed.Core.Components[0].Physical != want {
					t.Fatal("local producer physical demand differs", proposed.Core.Components[0].Physical, want)
				}
				if _, e = p.issueOriginalCreationFor(t.Context(), s, *proposed, payloads); !errors.Is(e, storagebudget.ErrAuthority) {
					t.Fatal("unqualified local producer issued a native member", e)
				}
			}
			after, e := c.ref.cell.q.InspectEnrollment(t.Context())
			storageTestMust(t, e)
			if !reflect.DeepEqual(before, after) || p.creation != nil || p.identityLaunch != nil {
				t.Fatal("read-only local producer changed Q or installed authority")
			}
			storageTestMust(t, p.finish(nil))
		})
	}
}

// Missing names must be proved on the original parent. An existing unsafe
// child must retain the pinning refusal rather than becoming a creation plan.
func TestStorageDraftMissingOriginalCustody(t *testing.T) {
	for _, mode := range []string{"absent", "empty", "complete", "file", "symlink", "nonprivate", "workflow-nonempty"} {
		t.Run(mode, func(t *testing.T) {
			parent := t.TempDir()
			path := filepath.Join(parent, "dialect-drafts")
			switch mode {
			case "file":
				privateFixture(t, path, "retained file\n")
			case "symlink":
				storageTestMust(t, os.Symlink(t.TempDir(), path))
			case "empty", "complete", "nonprivate", "workflow-nonempty":
				storageTestMust(t, os.Mkdir(path, 0700))
				if mode == "nonprivate" {
					storageTestMust(t, os.Chmod(path, 0755))
				}
				if mode == "complete" || mode == "workflow-nonempty" {
					privateFixture(t, filepath.Join(path, ".gitignore"), "retained custom ignore\n")
					workflow := ""
					if mode == "workflow-nonempty" {
						workflow = "retained lock bytes\n"
					}
					privateFixture(t, filepath.Join(path, "workflow.lock"), workflow)
				}
			}
			root, err := os.OpenRoot(parent)
			storageTestMust(t, err)
			defer func() { storageTestMust(t, root.Close()) }()
			before, beforeErr := root.Lstat("dialect-drafts")
			directory, ignore, workflow, err := storageDraftMissing(root)
			if mode == "file" || mode == "symlink" || mode == "nonprivate" || mode == "workflow-nonempty" {
				if !errors.Is(err, errStorageCustody) || directory || ignore || workflow {
					t.Fatal("unsafe original child became missing demand", directory, ignore, workflow, err)
				}
			} else {
				storageTestMust(t, err)
				if directory != (mode == "absent") || ignore != (mode != "complete") || workflow != (mode != "complete") {
					t.Fatal("original missing demand differs", directory, ignore, workflow)
				}
			}
			after, afterErr := root.Lstat("dialect-drafts")
			if mode == "absent" {
				if !errors.Is(beforeErr, os.ErrNotExist) || !errors.Is(afterErr, os.ErrNotExist) {
					t.Fatal("read-only absence inspection created a child", beforeErr, afterErr)
				}
			} else if beforeErr != nil || afterErr != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatal("read-only inspection replaced or changed original child", beforeErr, afterErr)
			}
			if mode == "complete" || mode == "workflow-nonempty" {
				ignore, err := os.ReadFile(filepath.Join(path, ".gitignore"))
				storageTestMust(t, err)
				workflow, err := os.ReadFile(filepath.Join(path, "workflow.lock"))
				storageTestMust(t, err)
				want := ""
				if mode == "workflow-nonempty" {
					want = "retained lock bytes\n"
				}
				if string(ignore) != "retained custom ignore\n" || string(workflow) != want {
					t.Fatal("inspection changed retained descendant bytes")
				}
			}
		})
	}
}

func TestStorageIdentityLocalProducerRefusalFinishesEpoch(t *testing.T) {
	cfg := ownerActualConfig(t)
	storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
	private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
	storageTestMust(t, os.Mkdir(private, 0700))
	// A file at the exact original name is unsafe, never positive absence.
	privateFixture(t, filepath.Join(private, "dialect-drafts"), "retained original\n")
	s, c, _ := ownerCrashConstruction(t, cfg)
	storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
	wrapper, err := c.forServer(s)
	storageTestMust(t, err)
	s.storageEnrollment = wrapper
	defer func() { storageTestMust(t, wrapper.Close()) }()
	before, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	c.owner.mu.Lock()
	active := c.owner.active
	c.owner.mu.Unlock()
	var p *storageEnrollmentEpoch
	err = func() (err error) {
		p, err = c.beginStartupEnrollment(t.Context())
		if err != nil {
			return err
		}
		// Exercise an early error after capture with no successful-bottom
		// finish. This finalizer also runs if a prerequisite asserts fatally.
		defer func() { err = p.finish(err) }()
		if err = p.selectOriginalRequester(t.Context(), s); err != nil {
			return err
		}
		if err = p.acquire(t.Context()); err != nil {
			return err
		}
		if err = p.captureInitialOriginal(t.Context()); err != nil {
			return err
		}
		proposed, payloads, err := p.planDemandedDraft(t.Context(), s)
		if !errors.Is(err, errStorageCustody) || proposed != nil || len(payloads) != 0 {
			t.Fatal("unsafe original draft did not refuse without a plan", err)
		}
		return err
	}()
	if !errors.Is(err, errStorageCustody) || p == nil {
		t.Fatal("early refusal was lost", err)
	}
	select {
	case <-p.done:
	default:
		t.Fatal("refused invocation did not signal drain")
	}
	if p.running || p.finishing || p.project != nil || p.work != nil || p.use != nil || len(p.holds) != 0 || len(p.sourceFiles) != 0 || len(p.sourceRoots) != 0 || p.creation != nil || p.identityLaunch != nil || p.qualified != nil {
		t.Fatal("early refusal retained epoch descriptions or authority")
	}
	for _, target := range p.targets {
		if target.named.file != nil {
			t.Fatal("early refusal retained original target description")
		}
	}
	for _, parent := range p.parents {
		if parent != nil {
			t.Fatal("early refusal retained original parent description")
		}
	}
	c.owner.mu.Lock()
	remaining := c.owner.active
	c.owner.admission.mu.Lock()
	enrollment, phase := c.owner.admission.enrollment, c.owner.admission.phase
	c.owner.admission.mu.Unlock()
	c.owner.mu.Unlock()
	if remaining != active || enrollment != nil || phase != storageAdmissionStartup {
		t.Fatal("early refusal retained owner admission", remaining, active, phase)
	}
	after, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	original, err := os.ReadFile(filepath.Join(private, "dialect-drafts"))
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) || string(original) != "retained original\n" {
		t.Fatal("refusal changed original Q or draft bytes")
	}
	storageTestMust(t, wrapper.Close())
}

func TestStorageIdentityLaunchFactualProjectionRetainsIndependentDebt(t *testing.T) {
	original := []storagebudget.Component{{Physical: storagebudget.Vector{Bytes: 100, Files: 1}, Attribution: storagebudget.Vector{Bytes: 100, Files: 1}, SettlementPhysical: storagebudget.Vector{Bytes: 200}, SettlementAttribution: storagebudget.Vector{Bytes: 300}, CapacityCount: 1, Capacity: [4]storagebudget.CapacityPromise{{Peak: storagebudget.CapacityVector{Bytes: 400, Inodes: 2}}}}}
	saved := append([]storagebudget.Component{}, original...)
	l := &storageIdentityLaunch{peak: storagebudget.CapacityVector{Bytes: 900, Inodes: 7}, applications: []storageIdentityLaunchApplication{{plan: storagebudget.ID{1}, action: 2, components: original}}}
	p := &storageEnrollmentEpoch{identityLaunch: l}
	pending, err := p.identityPendingComponents(t.Context(), l, storagebudget.StatusSnapshot{})
	storageTestMust(t, err)
	if !reflect.DeepEqual(pending, original) {
		t.Fatal("unsettled promise changed")
	}
	p.markApplication(storagebudget.ID{1}, 2, true, false)
	pending, err = p.identityPendingComponents(t.Context(), l, storagebudget.StatusSnapshot{})
	storageTestMust(t, err)
	if len(pending) != 1 || pending[0].Physical != (storagebudget.Vector{}) || pending[0].Attribution != (storagebudget.Vector{}) || pending[0].SettlementPhysical != (storagebudget.Vector{Bytes: 200}) || pending[0].SettlementAttribution != (storagebudget.Vector{Bytes: 300}) || pending[0].Capacity[0].Peak != (storagebudget.CapacityVector{Bytes: 400, Inodes: 2}) {
		t.Fatal("factual Q incorporation lost independent completion debt or repeated physical promise")
	}
	p.markApplication(storagebudget.ID{1}, 2, true, true)
	pending, err = p.identityPendingComponents(t.Context(), l, storagebudget.StatusSnapshot{})
	storageTestMust(t, err)
	if len(pending) != 0 || !reflect.DeepEqual(original, saved) || l.peak != (storagebudget.CapacityVector{Bytes: 900, Inodes: 7}) {
		t.Fatal("completion rewrote originals or refunded spent capacity")
	}
}

func TestStorageEnrollmentRecoveryObserverRetainsOriginalRows(t *testing.T) {
	original, _, _ := ownerAdvisoryFixture(false)
	candidate := ownerCatalogClone(original)
	// A new unrelated dictionary object sorts before the retained originals.
	// Only indices move; native/source identities and every reference remain.
	object := candidate.Objects[0]
	object.Role = 24
	object.Parent = storageOwnerNoObject
	object.Name = []byte("unrelated")
	object.Persistent.Object = 1
	candidate.Objects = append(candidate.Objects, object)
	storageTestMust(t, storageEnrollmentSortObjects(&candidate))
	savedOriginal, savedCandidate := ownerCatalogClone(original), ownerCatalogClone(candidate)
	if !storageEnrollmentOriginalObserverRows(original, candidate) {
		t.Fatal("exact retained rows with sorted indices refused")
	}
	for _, kind := range []string{"missing-root", "duplicate-root", "generation", "root-identity", "workspace", "category", "references", "capacity-reference", "native-object", "source", "membership"} {
		t.Run(kind, func(t *testing.T) {
			next := ownerCatalogClone(candidate)
			switch kind {
			case "missing-root":
				next.Roots = next.Roots[1:]
			case "duplicate-root":
				next.Roots = append(next.Roots, next.Roots[0])
			case "generation":
				next.Roots[0].Definition.Generation++
			case "root-identity":
				next.Roots[0].Definition.Identity[0] ^= 1
			case "workspace":
				next.Roots[0].Definition.Workspace = storagebudget.ID{88}
			case "category":
				next.Roots[0].Category++
			case "references":
				next.Roots[0].References = append(next.Roots[0].References, storagebudget.ID{88})
			case "capacity-reference":
				next.Roots[0].Definition.CapacityRefs[0].Generation++
			case "native-object":
				next.Objects[next.Roots[0].DirectoryObject].Persistent.Incarnation++
			case "source":
				next.Roots[0].Source.Path = []byte("/different-retained-root")
			case "membership":
				next.Roots[0].Membership.ProfileEvidence[0] ^= 1
			}
			if storageEnrollmentOriginalObserverRows(original, next) {
				t.Fatal("candidate broadened original sampling authority", kind)
			}
		})
	}
	if !reflect.DeepEqual(original, savedOriginal) || !reflect.DeepEqual(candidate, savedCandidate) {
		t.Fatal("read-only mapping comparison changed catalogs")
	}
}

// Required native success fixtures. The focused source checkpoint excludes
// these until the release-owned Version2 five-predicate prerequisite exists.
// No record/profile/observer is injected and no refusal or skip is success.
func TestStorageIdentityQualifiedOriginalKnownCuts(t *testing.T) {
	if storageEnrollmentInstalledNativeRecords == "[]" {
		t.Fatal("pending genuine Version2 native qualification")
	}
	for _, point := range []string{"identity-allocated-original-saved", "identity-payload-original-saved", "identity-target-original-observed", "identity-target-original-durable", "identity-terminal-before-settled", "identity-checkpoint-before-retired", "identity-retired-before-receipt", "identity-catalog/lin4/plan-prepared/persist", "identity-catalog/lin4/completion-prepared/persist", "identity-catalog/lin4/completion-active/persist", "identity-rename-before-observed-page", "identity-catalog/m1-proposed/stamp-anchor/stamp-prepared/persist", "identity-catalog/m1-proposed/stamp-anchor/stamp-active/persist", "identity-catalog/lin4/from-prepared/persist", "identity-catalog/lin4/from-active/persist"} {
		t.Run(point, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			storageTestMust(t, os.Chmod(cfg.ProjectDir, 0700))
			storageTestMust(t, os.Mkdir(filepath.Join(cfg.ProjectDir, startupIdentityDir), 0700))
			legacyFrom := point == "identity-rename-before-observed-page" || strings.Contains(point, "stamp-anchor") || strings.Contains(point, "lin4/from-")
			if legacyFrom {
				info, e := os.Stat(cfg.ProjectDir)
				storageTestMust(t, e)
				old := identityRecord{ID: strings.Repeat("9", 64), Path: cfg.ProjectDir, Folder: identityKey(info), From: digestOf([]byte(cfg.ProjectDir))}
				privateFixture(t, filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName), string(old.line()))
			}
			cut := errors.New("handwritten original identity recovery cut")
			_, c, closeOriginal := ownerCrashConstructionCloseCheck(t, cfg, func(err error) {
				if err != nil && !errors.Is(err, cut) {
					t.Error(err)
				}
			})
			storageTestMust(t, c.loadOrEnrollOwner(t.Context()))
			beforeOwner := c.ref.cell.ownerCatalog.OriginalOwner
			beforeWorkspaces := append([]storageOwnerWorkspace{}, c.ref.cell.ownerCatalog.Workspaces...)
			c.enrollmentFault = &storageEnrollmentFault{point: point, err: cut}
			if err := c.completeOriginalIdentity(t.Context()); !errors.Is(err, cut) {
				t.Fatal("actual original execution did not reach known cut", err)
			}
			record, claim := *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim
			a, e := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
			storageTestMust(t, e)
			b, e := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.b"))
			storageTestMust(t, e)
			pair, e := storageCompositionReadPair(a, b, record, claim)
			storageTestMust(t, e)
			rows := pair.Banks[pair.Selected].Lineage.Rows.LineagePlans
			if len(rows) != 1 {
				t.Fatal("cut lost original immutable P")
			}
			initial := append([]byte{}, rows[0].IdentityInitial...)
			closeOriginal()
			next := ownerActualNew(t, cfg)
			n := next.storageConstruction
			raw, e := os.ReadFile(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName))
			storageTestMust(t, e)
			expected, e := parseIdentity(initial)
			storageTestMust(t, e)
			if legacyFrom {
				expected.From = ""
			}
			want := expected.line()
			if !bytes.Equal(raw, want) || *n.ref.cell.ownerRecord != record || !reflect.DeepEqual(n.ref.cell.ownerCatalog.OriginalOwner, beforeOwner) || !reflect.DeepEqual(n.ref.cell.ownerCatalog.Workspaces, beforeWorkspaces) {
				t.Fatal("known cut redraw/adoption or immutable accounting change")
			}
			q, e := n.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			for _, slot := range q.View.Slots {
				if storageUnretired(slot) {
					t.Fatal("known cut did not persist actual settlement")
				}
			}
			held, e := os.ReadDir(filepath.Join(cfg.ProjectDir, startupIdentityDir))
			storageTestMust(t, e)
			for _, entry := range held {
				if strings.HasPrefix(entry.Name(), identityStagingPrefix) {
					t.Fatal("known cut left or reallocated a stage")
				}
			}
		})
	}
}
func TestStorageIdentityQualifiedLazyPrivateDraftLifecycle(t *testing.T) {
	if storageEnrollmentInstalledNativeRecords == "[]" {
		t.Fatal("pending genuine Version2 native qualification")
	}
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	c := s.storageConstruction
	record := *c.ref.cell.ownerRecord
	workspace := s.storageEnrollment.workspace
	release, err := s.lockDialectDraftWorkflow(t.Context())
	storageTestMust(t, err)
	release()
	c.ref.cell.mu.Lock()
	endpoint := c.ref.cell.enrollmentEndpoint
	catalog := c.ref.cell.ownerCatalog
	c.ref.cell.mu.Unlock()
	if endpoint == nil || endpoint.Phase != 2 {
		t.Fatal("lazy J4 did not finish actual child enrollment")
	}
	found := false
	for _, row := range catalog.Roots {
		if row.Source.Kind == 8 && (row.Role == 3 || row.Role == 6) {
			if row.Source.ParentRoot == (storagebudget.ID{}) || row.DomainObject != storageOwnerNoObject || len(row.References) == 0 {
				t.Fatal("child lost parent delegation or references")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("lazy demand substituted an independent/private singleton route")
	}
	private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
	raw, err := os.ReadFile(filepath.Join(private, "dialect-drafts", ".gitignore"))
	storageTestMust(t, err)
	if string(raw) != "*\n" {
		t.Fatal("actual ignore bytes differ")
	}
	identity, err := os.ReadFile(filepath.Join(private, startupIdentityName))
	storageTestMust(t, err)
	parsed, err := parseIdentity(identity)
	storageTestMust(t, err)
	if parsed.ID == digestOf([]byte(cfg.ProjectDir)) {
		t.Fatal("lazy identity was seeded with immutable accounting key")
	}
	before, err := c.ref.cell.q.InspectAction(t.Context())
	storageTestMust(t, err)
	release, err = s.lockDialectDraftWorkflow(t.Context())
	storageTestMust(t, err)
	release()
	after, err := c.ref.cell.q.InspectAction(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("complete lazy descendants issued an extra Q effect")
	}
	storageTestMust(t, s.Close())
	next := ownerActualNew(t, cfg)
	if *next.storageConstruction.ref.cell.ownerRecord != record || next.storageEnrollment.workspace != workspace {
		t.Fatal("Close/New changed original custody or workspace")
	}
	again, err := os.ReadFile(filepath.Join(private, startupIdentityName))
	storageTestMust(t, err)
	if !bytes.Equal(again, identity) {
		t.Fatal("Close/New rewrote the legitimately issued identity")
	}
	release, err = next.lockDialectDraftWorkflow(t.Context())
	storageTestMust(t, err)
	release()
}

// A real strict Q with a handwritten comparison-only J operand. This fixture
// tests request/projection metadata; it creates no native issuer, qualification,
// host publication, identity receipt, or successful application claim.
func storageIdentityTerminalReplayFixture(t *testing.T) (*storagebudget.Coordinator, storageIdentityIntent, storagebudget.Request, storagebudget.Token) {
	t.Helper()
	path := t.TempDir()
	storageTestMust(t, os.Chmod(path, 0700))
	root, err := os.OpenRoot(path)
	storageTestMust(t, err)
	lock, err := root.OpenFile("lock", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	storageTestMust(t, err)
	q, err := storagebudget.OpenHeld(storagebudget.HeldAnchor{Root: root, LockWitness: lock, Installation: storagebudget.ID{90}, Binding: storagebudget.Digest{91}})
	storageTestMust(t, err)
	t.Cleanup(func() {
		storageTestMust(t, q.Close())
		storageTestMust(t, lock.Close())
		storageTestMust(t, root.Close())
	})
	var policy storagebudget.Policy
	policy.ObservationMaxAge = time.Minute
	policy.Installation.Total = storagebudget.Vector{Bytes: 1 << 30, Files: 100, Directories: 100}
	policy.Workspace.Total = storagebudget.Vector{Bytes: 300, Files: 10, Directories: 10}
	for i := 0; i < storagebudget.Categories; i++ {
		policy.Installation.Category[i] = policy.Installation.Total
		policy.Workspace.Category[i] = policy.Workspace.Total
	}
	storageTestMust(t, q.InitializeHeld(t.Context(), policy))
	fs := storagebudget.Filesystem{ID: storagebudget.ID{5}, Identity: storagebudget.Digest{11}, Generation: 1, InodeModel: storagebudget.InodesFinite, CapabilityEvidence: storagebudget.Digest{80}, ByteConstraint: storagebudget.ID{8}, InodeConstraint: storagebudget.ID{8}}
	constraint := storagebudget.CapacityConstraint{ID: storagebudget.ID{8}, Identity: storagebudget.Digest{81}, Generation: 1, Resources: storagebudget.CapacityBytes | storagebudget.CapacityInodes, Headroom: storagebudget.CapacityVector{Bytes: 100, Inodes: 10}}
	registered := storagebudget.Root{ID: storagebudget.ID{6}, Owner: storagebudget.Digest{91}, Identity: storagebudget.Digest{10}, Filesystem: fs.ID, Workspace: storagebudget.ID{7}, Generation: 1, Locator: "private", CapacityCount: 1, CapacityRefs: [4]storagebudget.CapacityRef{{Constraint: constraint.ID, Generation: 1}}}
	registered.CapacityBinding, err = storagebudget.CapacityBindingFor(registered, fs, []storagebudget.CapacityConstraint{constraint})
	storageTestMust(t, err)
	control := registered
	control.ID, control.Workspace, control.Locator = storagebudget.ID{9}, storagebudget.ID{}, "control"
	// These are separate physical roots; Q rejects duplicate root identities.
	control.Identity = storagebudget.Digest{12}
	control.CapacityBinding, err = storagebudget.CapacityBindingFor(control, fs, []storagebudget.CapacityConstraint{constraint})
	storageTestMust(t, err)
	storageTestMust(t, q.Register(t.Context(), 1, storagebudget.Registration{Roots: []storagebudget.Root{registered, control}, Workspaces: []storagebudget.Workspace{{ID: registered.Workspace, Generation: 1, Limits: policy.Workspace}}, Filesystems: []storagebudget.Filesystem{fs}, CapacityConstraints: []storagebudget.CapacityConstraint{constraint}}))
	var controlUsage [storagebudget.Categories]storagebudget.Vector
	controlUsage[storagebudget.Control] = storagebudget.Vector{Bytes: storagebudget.MetadataBudget, Files: 16, Directories: 1}
	storageTestMust(t, q.PublishCensus(t.Context(), 1, storagebudget.Census{Evidence: storagebudget.Digest{82}, Roots: []storagebudget.RootMeasurement{{ID: registered.ID, Generation: 1}, {ID: control.ID, Generation: 1, Usage: controlUsage}}, Workspaces: []storagebudget.WorkspaceMeasurement{{ID: registered.Workspace, Generation: 1}}}))
	r := storageIdentityIntentState(storageIdentityTargetDurable)
	r.Core.AdmissionEpoch = 1
	r.Core.Components = []storagebudget.Component{{Root: registered.ID, RootGeneration: 1, Owner: registered.Owner, Filesystem: fs.ID, FilesystemGeneration: 1, Workspace: registered.Workspace, WorkspaceGeneration: 1, Category: storagebudget.Control, Physical: storagebudget.Vector{Bytes: 100, Files: 1}, Attribution: storagebudget.Vector{Bytes: 100, Files: 1}, SettlementPhysical: storagebudget.Vector{Bytes: 200}, SettlementAttribution: storagebudget.Vector{Bytes: 200}, CapacityCount: 1, CapacityBinding: registered.CapacityBinding, Capacity: [4]storagebudget.CapacityPromise{{Constraint: constraint.ID, Generation: 1, Peak: storagebudget.CapacityVector{Bytes: 400, Inodes: 2}}}}}
	r.Core.RootIdentities, r.Core.FilesystemIdentities = []storagebudget.Digest{registered.Identity}, []storagebudget.Digest{fs.Identity}
	request, err := storageIdentityRequest(r, registered.Owner)
	storageTestMust(t, err)
	initial, err := q.Reserve(t.Context(), request, []storagebudget.CapacityObservation{{Constraint: constraint.ID, Generation: 1, Available: storagebudget.CapacityVector{Bytes: 1 << 30, Inodes: 100}, At: time.Now(), KnownResources: storagebudget.CapacityBytes | storagebudget.CapacityInodes}})
	storageTestMust(t, err)
	host, err := q.InspectHostState(t.Context())
	storageTestMust(t, err)
	staging, err := q.Advance(t.Context(), initial, registered.Owner, storagebudget.Staging, r.Core.CASBinding)
	storageTestMust(t, err)
	r.Core.PolicyRevision = host.Slots[initial.Slot].Ticket.PolicyRevision
	r.Core.PreviousToken, r.Core.Token = initial, staging
	r.Core.Phase, r.Core.Outcome = storageIntentSettling, storageIntentPublished
	r.Core.Changes, err = storageIdentitySettlementChanges(r)
	storageTestMust(t, err)
	r.Core.Evidence, err = storageOwnerDigest("desk-storage-identity-publication-v1", storageIdentityPublicationMaterial(r))
	storageTestMust(t, err)
	storageTestMust(t, r.validate())
	terminal, err := q.Settle(t.Context(), storagebudget.SettlementFact{Token: staging, Owner: registered.Owner, Evidence: r.Core.Evidence, Changes: r.Core.Changes})
	storageTestMust(t, err)
	return q, r, request, terminal
}

func TestStorageIdentityRecoveredTerminalInitializesFactualProjection(t *testing.T) {
	q, r, request, terminal := storageIdentityTerminalReplayFixture(t)
	inspection, err := q.InspectAction(t.Context())
	storageTestMust(t, err)
	row := inspection.View.Slots[terminal.Slot]
	if row.PersistedApplied == terminal || row.Ticket.State != storagebudget.Terminal {
		t.Fatal("fixture must be terminal replay, not disk-applied")
	}
	c := &storageBudgetConstruction{ref: &storageInstallationRef{cell: &storageInstallation{q: q, binding: request.Owner}}}
	p := &storageEnrollmentEpoch{c: c, initialInspection: &inspection, identityPlan: &storageOwnerV5LineagePlan{PlanID: storagebudget.ID{1}}}
	m := &storageIdentityLive{phase: p, record: r, request: request, admitted: terminal, recovering: true}
	m.self, p.identity = m, m
	// This is the initializer called by prepareIdentityLaunch. No manual
	// markApplication or pre-set settled flag stands in for recovery initialization.
	application, err := p.originalIdentityApplication(t.Context(), p.identityPlan.PlanID, r.Action, request.Components, false)
	storageTestMust(t, err)
	if !application.settled || application.complete {
		t.Fatal("terminal incorporation confused with durable C/S/F")
	}
	l := &storageIdentityLaunch{peak: storagebudget.CapacityVector{Bytes: 900, Inodes: 7}, applications: []storageIdentityLaunchApplication{application}}
	status, err := q.StatusSnapshot(t.Context())
	storageTestMust(t, err)
	pending, err := p.identityPendingComponents(t.Context(), l, status)
	storageTestMust(t, err)
	if len(pending) != 1 || pending[0].Physical != (storagebudget.Vector{}) || pending[0].Attribution != (storagebudget.Vector{}) || pending[0].SettlementPhysical != (storagebudget.Vector{Bytes: 200}) || pending[0].SettlementAttribution != (storagebudget.Vector{Bytes: 200}) || pending[0].Capacity[0].Peak != (storagebudget.CapacityVector{Bytes: 400, Inodes: 2}) || l.peak != (storagebudget.CapacityVector{Bytes: 900, Inodes: 7}) {
		t.Fatal("recovery changed original debt/peaks or duplicated ordinary demand")
	}
	if len(status.Workspaces) != 1 || status.Workspaces[0].Usage[storagebudget.Control].Bytes != 100 {
		t.Fatal("Q did not incorporate original terminal replay")
	}
	usage := status.Workspaces[0].Usage[storagebudget.Control]
	if usage.Bytes+pending[0].SettlementAttribution.Bytes > 300 || usage.Bytes+request.Components[0].Attribution.Bytes+pending[0].SettlementAttribution.Bytes <= 300 {
		t.Fatal("policy does not isolate duplicate ordinary demand")
	}
	after, err := q.InspectAction(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(inspection, after) || !reflect.DeepEqual(application.components, request.Components) {
		t.Fatal("initializer wrote Q or rewrote original board")
	}
}

func TestStorageIdentityOriginalAppliedRequestRecoveryCuts(t *testing.T) {
	q, r, request, terminal := storageIdentityTerminalReplayFixture(t)
	before, err := q.InspectHostState(t.Context())
	storageTestMust(t, err)
	storageTestMust(t, storageOriginalActionRequest(t.Context(), q, r.Core, request, before, terminal))
	storageTestMust(t, q.Checkpoint(t.Context()))
	status, err := q.InspectHostState(t.Context())
	storageTestMust(t, err)
	if _, err = q.Manifest(t.Context(), terminal, request.Owner); !errors.Is(err, storagebudget.ErrStale) {
		t.Fatal("strict Q did not retire the manifest", err)
	}
	for _, phase := range []storageIntentPhase{storageIntentSettling, storageIntentSettled, storageIntentRetired} {
		core := r.Core
		core.Phase = phase
		if phase >= storageIntentSettled {
			core.PreviousToken, core.Token = r.Core.Token, terminal
		}
		if phase == storageIntentRetired {
			core.RetirementEpoch = status.Epoch
		}
		storageTestMust(t, storageOriginalActionRequest(t.Context(), q, core, request, status, terminal))
		wrong := request
		wrong.Entries = append([]storagebudget.Entry{}, request.Entries...)
		wrong.Entries[0].Target = "other.json"
		if err := storageOriginalActionRequest(t.Context(), q, core, wrong, status, terminal); err == nil {
			t.Fatal("different immutable request accepted", phase)
		}
	}
	// Strict full-J/native/Prepared-C/S/F success is separately exercised by the
	// qualified known-cut fixture; these metadata cuts manufacture no receipt.
}

// This required application fixture remains pending genuine installed native
// qualification. It uses actual New, an actual retained home child and original
// J/Q/native effects; no child Server/ProjectRoot or issuer is fabricated.
func TestStorageIdentityQualifiedRetainedChildRestartViaPrimary(t *testing.T) {
	if storageEnrollmentInstalledNativeRecords == "[]" {
		t.Fatal("pending genuine Version2 native qualification")
	}
	for _, point := range []string{"identity-terminal-before-settled", "identity-checkpoint-before-retired", "identity-retired-before-receipt", "identity-catalog/lin4/completion-prepared/persist", "local-private-effect-captured", "local-private-all-durable", "creation-checkpoint-before-retired"} {
		t.Run(point, func(t *testing.T) {
			cfg, registry := homeFixture(t)
			cfg.CodexBin, cfg.DevMode, cfg.Port = "off", true, testPort
			infrastructureRequireNative(t, cfg.DeskConfigDir)
			entry := registry.Desks[0]
			entry.ID, entry.Name, entry.Folder = strings.Repeat("d", 32), "Second", "second"
			entry.HistoryIdentity, entry.RunnerWorkspace = "/original/second", strings.Repeat("e", 64)
			childPath := filepath.Join(filepath.Dir(cfg.DeskConfigDir), entry.Folder)
			storageTestMust(t, os.MkdirAll(childPath, 0700))
			privateFixture(t, filepath.Join(childPath, projectConfigName), `{"deskConfigVersion":1}`)
			identityCut := strings.HasPrefix(point, "identity-")
			if !identityCut {
				// Existing private roots use local J5 only. Identity rows instead
				// leave private absent: real draft demand must create/enroll it via
				// J4, then its createdPrivate continuation issues B's identity.
				storageTestMust(t, os.MkdirAll(filepath.Join(childPath, startupIdentityDir, "data"), 0700))
				privateFixture(t, filepath.Join(childPath, startupIdentityDir, dataLocationName), `{"version":1,"path":"data"}`)
				marker, err := json.Marshal(dataMarker{Version: 1, Owner: registry.Owner})
				storageTestMust(t, err)
				privateFixture(t, filepath.Join(childPath, startupIdentityDir, "data", dataMarkerName), string(marker))
			}
			registry.Desks = append(registry.Desks, entry)
			encoded, err := json.Marshal(registry)
			storageTestMust(t, err)
			privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(encoded))
			cut := errors.New("handwritten retained-child original recovery cut")
			parent, err := New(cfg)
			storageTestMust(t, err)
			closed := false
			closeParent := func() {
				if closed {
					return
				}
				closed = true
				if e := parent.Close(); e != nil && !errors.Is(e, cut) {
					t.Error(e)
				}
			}
			t.Cleanup(closeParent)
			if parent.storageReadOnly || parent.storageConstruction == nil {
				t.Fatal("actual primary not ready", parent.storageProblem)
			}
			child := parent.desks[entry.ID]
			if child == nil || child.storageEnrollment == nil || child.storageConstruction != nil {
				t.Fatal("actual retained child missing or independently owned")
			}
			if identityCut {
				if _, err := os.Lstat(filepath.Join(childPath, startupIdentityDir)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("identity cut requires actual lazy private creation", err)
				}
			}
			c := parent.storageConstruction
			record := *c.ref.cell.ownerRecord
			owner := c.ref.cell.ownerCatalog.OriginalOwner
			workspaces := append([]storageOwnerWorkspace{}, c.ref.cell.ownerCatalog.Workspaces...)
			primaryIdentity, err := os.ReadFile(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName))
			storageTestMust(t, err)
			ready, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageReadyName))
			storageTestMust(t, err)
			c.enrollmentFault = &storageEnrollmentFault{point: point, err: cut}
			release, err := child.lockDialectDraftWorkflow(t.Context())
			if release != nil {
				release()
			}
			if !errors.Is(err, cut) {
				t.Fatal("actual child did not reach original known cut", err)
			}
			qBefore, err := c.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, err)
			raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageIntentName))
			storageTestMust(t, err)
			var original storageIntentRecord
			var identity *storageIdentityIntent
			var local *storageCreationIntent
			for slot := 0; slot < storagebudget.Slots; slot++ {
				start := slot * 2 * storageIntentPage
				selected, e := storageSelectCreationHost(raw[start:start+storageIntentPage], raw[start+storageIntentPage:start+2*storageIntentPage], slot, record.Installation, record.QBinding)
				storageTestMust(t, e)
				if selected.Identity != nil && strings.HasPrefix(point, "identity-") {
					matched := false
					// The independently read original P binds this exact child's native
					// project/private/workspace relation; A's P cannot explain B's action.
					lineage, e := c.owner.identityLineage(t.Context())
					if e != nil && strings.Contains(point, "completion-prepared") {
						a, e0 := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
						storageTestMust(t, e0)
						b, e0 := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.b"))
						storageTestMust(t, e0)
						pair, e0 := storageCompositionReadPair(a, b, record, c.ref.cell.ownerClaim)
						storageTestMust(t, e0)
						lineage = pair.Banks[pair.Selected].Lineage
					} else {
						storageTestMust(t, e)
					}
					for _, p := range lineage.Rows.LineagePlans {
						d, e := storageOwnerV5PlanDigest(p)
						storageTestMust(t, e)
						matched = matched || d == selected.Identity.PlanDigest && string(p.AccountingOwnerKey) == entry.RunnerWorkspace && string(p.HistoryIdentity) == entry.HistoryIdentity
					}
					if matched {
						copy := *selected.Identity
						identity = &copy
						original = copy.Core
					}
				}
				if selected.Creation != nil && selected.Creation.Kind == storageCreationLocalPrivate && selected.Creation.SelectedPath == filepath.Join(childPath, dialectDraftDir) {
					copy := *selected.Creation
					local = &copy
					original = copy.Core
				}
			}
			if identityCut && (identity == nil || local != nil) || !identityCut && (local == nil || identity != nil) {
				t.Fatal("known cut lost B's route-specific original J")
			}
			closeParent()
			// Restart through A. Parent startup must recover B before census/handoff;
			// construction of the later B wrapper is not an alternate recovery route.
			next := ownerActualNew(t, cfg)
			n := next.storageConstruction
			if *n.ref.cell.ownerRecord != record || !reflect.DeepEqual(n.ref.cell.ownerCatalog.OriginalOwner, owner) || !reflect.DeepEqual(n.ref.cell.ownerCatalog.Workspaces, workspaces) {
				t.Fatal("retained A/B accounting changed")
			}
			actualPrimary, e := os.ReadFile(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName))
			storageTestMust(t, e)
			actualReady, e := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageReadyName))
			storageTestMust(t, e)
			if !bytes.Equal(primaryIdentity, actualPrimary) || !bytes.Equal(ready, actualReady) {
				t.Fatal("B recovery changed A identity or Ready")
			}
			after, e := n.ref.cell.q.InspectAction(t.Context())
			storageTestMust(t, e)
			ticket := after.View.Slots[original.Token.Slot]
			beforeTicket := qBefore.View.Slots[original.Token.Slot].Ticket
			if ticket.Ticket.State != storagebudget.Terminal || ticket.Ticket.Token.ID != beforeTicket.Token.ID || ticket.Ticket.Token.Sequence != beforeTicket.Token.Sequence || ticket.PersistedApplied != ticket.Ticket.Token || ticket.PersistedNextSequence <= ticket.Ticket.Token.Sequence || ticket.Ticket.Operation != beforeTicket.Operation {
				t.Fatal("B continuation substituted a ticket or did not settle")
			}
			for _, row := range after.View.Slots {
				if storageUnretired(row) {
					t.Fatal("B recovery handed off unresolved original work")
				}
			}
			if identity != nil {
				f, e := os.Open(filepath.Join(childPath, startupIdentityDir, startupIdentityName))
				storageTestMust(t, e)
				native, e := storageAdvisoryPersistentHeld(t.Context(), f)
				storageTestMust(t, e)
				storageTestMust(t, f.Close())
				if native != identity.Target {
					t.Fatal("B recovery repeated native identity publication")
				}
			}
			if local != nil {
				for _, effect := range local.Effects {
					if effect.State == storageCreationOwed {
						continue
					}
					path := filepath.Join(childPath, dialectDraftDir)
					if effect.Kind != 1 {
						path = filepath.Join(path, effect.Name)
					}
					f, e := os.Open(path)
					storageTestMust(t, e)
					native, e := storageAdvisoryPersistentHeld(t.Context(), f)
					storageTestMust(t, e)
					storageTestMust(t, f.Close())
					if native != effect.Native {
						t.Fatal("B recovery repeated a captured local effect")
					}
				}
				ignore, e := os.ReadFile(filepath.Join(childPath, dialectDraftDir, ".gitignore"))
				storageTestMust(t, e)
				lock, e := os.ReadFile(filepath.Join(childPath, dialectDraftDir, "workflow.lock"))
				storageTestMust(t, e)
				if string(ignore) != "*\n" || len(lock) != 0 {
					t.Fatal("B continuation lost exact local payloads")
				}
			}
		})
	}
}
