package desk

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

var ownerReviewAnchorNames = []string{"lock", "checkpoint.a", "checkpoint.b", "tickets.a", "tickets.b", "manifests"}

func ownerReviewDurableState(t *testing.T, root *os.Root) map[string]string {
	t.Helper()
	names := []string{storageClaimName, storageRecordName, "storage-owner.a", "storage-owner.b", storageIntentName, storageReadyName}
	for _, name := range ownerReviewAnchorNames {
		names = append(names, storageAnchorName+"/"+name)
	}
	out := map[string]string{}
	for _, name := range names {
		if name == storageReadyName {
			if info, err := root.Lstat(name); err == nil {
				links, _ := linkCount(info)
				out[name+"#shape"] = fmt.Sprintf("%s/%s/%d", info.Mode(), identityKey(info), links)
				if info.IsDir() {
					out[name] = "directory"
					continue
				}
			}
		}
		b, err := root.ReadFile(name)
		if errors.Is(err, os.ErrNotExist) {
			out[name] = "missing"
			continue
		}
		if err != nil {
			t.Fatal(name, err)
		}
		out[name] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	return out
}

func TestStorageOwnerOriginalAnchorControls(t *testing.T) {
	cfg := ownerActualConfig(t)
	_, c, _ := ownerCrashConstruction(t, cfg)
	if err := c.loadOrEnrollOwner(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range ownerReviewAnchorNames {
		n, err := c.openOriginalAnchorControl(t.Context(), name)
		if err != nil {
			t.Fatal(name, err)
		}
		if n.root != c.anchor || n.name != name {
			t.Fatal("anchor witness selected wrong parent")
		}
		if err = n.file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{storageClaimName, "../lock", "arbitrary"} {
		if _, err := c.openOriginalAnchorControl(t.Context(), name); err == nil {
			t.Fatal("arbitrary anchor control accepted", name)
		}
	}
}

func TestStorageOwnerActualAnchorControlRefusals(t *testing.T) {
	requireStorageLine(t)
	for _, name := range ownerReviewAnchorNames {
		for _, kind := range []string{"missing", "substituted", "partial"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				cfg := ownerActualConfig(t)
				s := ownerActualNew(t, cfg)
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(cfg.DeskConfigDir, storageAnchorName, name)
				switch kind {
				case "missing":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				case "substituted":
					raw, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.Rename(path, filepath.Join(cfg.DeskConfigDir, "review-original-"+name)); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				case "partial":
					if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
						t.Fatal(err)
					}
				}
				root, err := os.OpenRoot(cfg.DeskConfigDir)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				before := ownerReviewDurableState(t, root)
				next, err := New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer next.Close()
				if !next.storageReadOnly || next.storageEnrollment != nil {
					t.Fatal("foreign/uncertain original anchor activated", name, kind)
				}
				if !reflect.DeepEqual(before, ownerReviewDurableState(t, root)) {
					t.Fatal("refusal changed Q/catalog/obligation bytes", name, kind)
				}
			})
		}
	}
}

func ownerReviewHome(t *testing.T, private bool) (Config, deskHomeEntry) {
	t.Helper()
	cfg, registry := homeFixture(t)
	cfg.CodexBin = "off"
	cfg.DevMode = true
	cfg.Port = testPort
	infrastructureRequireNative(t, cfg.DeskConfigDir)
	entry := registry.Desks[0]
	entry.ID = strings.Repeat("d", 32)
	entry.Name = "Second"
	entry.Folder = "second"
	entry.HistoryIdentity = "/original/second"
	entry.RunnerWorkspace = strings.Repeat("e", 64)
	project := filepath.Join(filepath.Dir(cfg.DeskConfigDir), entry.Folder)
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(project, projectConfigName), `{"deskConfigVersion":1}`)
	if private {
		if err := os.MkdirAll(filepath.Join(project, ".desk-private", "data"), 0700); err != nil {
			t.Fatal(err)
		}
		privateFixture(t, filepath.Join(project, ".desk-private", dataLocationName), `{"version":1,"path":"data"}`)
		marker, err := json.Marshal(dataMarker{Version: 1, Owner: registry.Owner})
		if err != nil {
			t.Fatal(err)
		}
		privateFixture(t, filepath.Join(project, ".desk-private", "data", dataMarkerName), string(marker))
	}
	registry.Desks = append(registry.Desks, entry)
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(raw))
	return cfg, entry
}

func TestStorageOwnerActualNamedProjectReplacement(t *testing.T) {
	requireStorageLine(t)
	for _, private := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent-private", true: "retained-private"}[private], func(t *testing.T) {
			cfg, entry := ownerReviewHome(t, private)
			parent := ownerActualNew(t, cfg)
			c := parent.storageConstruction
			child := parent.desks[entry.ID]
			if child == nil || child.storageReadOnly || child.storageEnrollment == nil {
				t.Fatal("legitimate original child refused")
			}
			if err := child.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.ref.cell.current(); err != nil {
				t.Fatal("legitimate child Close revoked original custody", err)
			}
			path := filepath.Join(parent.home.Path, entry.Folder)
			if err := os.Rename(path, filepath.Join(parent.home.Path, "original-second")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			privateFixture(t, filepath.Join(path, projectConfigName), `{"deskConfigVersion":1}`)
			before := ownerReviewDurableState(t, c.original.config)
			if err := c.sources.currentProjects(t.Context()); err == nil {
				t.Fatal("named original project replacement was current")
			}
			if err := c.ref.cell.current(); err == nil {
				t.Fatal("installation ignored changed original project edge")
			}
			pinned, err := OpenProjectRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			nextCfg := parent.cfg
			nextCfg.parent = parent
			nextCfg.Root = pinned
			nextCfg.ProjectDir = ""
			nextCfg.deskID = ""
			replacement, err := New(nextCfg)
			if err == nil {
				defer replacement.Close()
				if !replacement.storageReadOnly || replacement.storageEnrollment != nil {
					t.Fatal("replacement child borrowed old installation")
				}
			}
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, c.original.config)) {
				t.Fatal("replacement child changed original Q/catalog/obligations")
			}
		})
	}
}

func TestStorageOwnerActualNamedHomeReplacement(t *testing.T) {
	requireStorageLine(t)
	cfg, entry := ownerReviewHome(t, false)
	parent := ownerActualNew(t, cfg)
	c := parent.storageConstruction
	home := parent.home.Path
	if err := os.Rename(home, home+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, entry.Folder), 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(home, entry.Folder, projectConfigName), `{"deskConfigVersion":1}`)
	before := ownerReviewDurableState(t, c.original.config)
	if err := c.sources.currentProjects(t.Context()); err == nil {
		t.Fatal("replaced named home stayed current")
	}
	if err := c.current(t.Context(), false); err == nil {
		t.Fatal("construction accepted replacement home")
	}
	pinned, err := OpenProjectRoot(filepath.Join(home, entry.Folder))
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	foreign := &Server{cfg: Config{parent: parent}, project: pinned, root: pinned.own.root, projectDir: pinned.dir, configDir: parent.configDir, assistant: parent.assistant, retainedArtifactProviders: parent.retainedArtifactProviders}
	if _, err = c.forServer(foreign); err == nil {
		t.Fatal("replacement-home child borrowed original installation")
	}
	if !reflect.DeepEqual(before, ownerReviewDurableState(t, c.original.config)) {
		t.Fatal("home replacement refusal mutated original durable state")
	}
}

func TestStorageOwnerActualUnretiredRestartPreservesObligation(t *testing.T) {
	requireStorageLine(t)
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "unretired", true: "terminal-not-applied"}[terminal], func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s := ownerActualNew(t, cfg)
			c := s.storageConstruction
			var root *storageRootBinding
			for _, r := range c.owner.roots {
				if r.configControl {
					root = r
				}
			}
			plan := storageSingleFilePlan{Root: root, Target: "unretired.json", Stage: ".unretired-stage", Workflow: "owner-review", Operation: "uncertain", ExpectedAbsent: true, Bytes: []byte("original obligation"), ObjectLimit: 4096}
			admission, err := c.owner.admitSingleFile(t.Context(), plan)
			if err != nil {
				t.Fatal(err)
			}
			if terminal {
				c.owner.fault = &storageBudgetFault{Name: "settle", Phase: "after"}
				if err = admission.Write(t.Context()); err == nil {
					t.Fatal("terminal fault not reached")
				}
			}
			if err = admission.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := c.ref.cell.q.InspectHost(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, slot := range before.Slots {
				if storageUnretired(slot) {
					found = true
					if terminal && (slot.Ticket.State != storagebudget.Terminal || slot.PersistedApplied == slot.Ticket.Token) {
						t.Fatal("case did not retain exact unapplied terminal")
					}
				}
			}
			if !found {
				t.Fatal("case did not retain original obligation")
			}
			// Keep an independently owned read-only description of the exact
			// original config across Server.Close. It never opens Q or supplies
			// a production reference. The full validated pre-close inspection
			// above identifies the ticket/applied/next-sequence obligations;
			// equal raw Q bytes and identities below prove their conservation.
			inspection, err := os.OpenRoot(cfg.DeskConfigDir)
			if err != nil {
				t.Fatal(err)
			}
			defer inspection.Close()
			originalInfo, err := c.original.config.Stat(".")
			if err != nil {
				t.Fatal(err)
			}
			inspectionInfo, err := inspection.Stat(".")
			if err != nil || !os.SameFile(originalInfo, inspectionInfo) {
				t.Fatal("inspection did not retain original config", err)
			}
			snapshot := func() map[string]string {
				t.Helper()
				state := map[string]string{}
				names := []string{storageClaimName, storageRecordName, "storage-owner.a", "storage-owner.b", storageIntentName, storageReadyName, storageBootstrapName, storageUseName, ".home-use.lock", ".data.lock", ".data-work.lock"}
				for _, name := range ownerReviewAnchorNames {
					names = append(names, storageAnchorName+"/"+name)
				}
				for _, name := range names {
					info, err := inspection.Lstat(name)
					if err != nil || !storagePrivate(info, false) || info.Size() < 0 || info.Size() > 33554432 {
						t.Fatal("original control shape unavailable", name, err)
					}
					data, err := inspection.ReadFile(name)
					if err != nil || int64(len(data)) != info.Size() {
						t.Fatal("original control bytes unavailable", name, err)
					}
					links, ok := linkCount(info)
					if !ok {
						t.Fatal("original control identity unavailable", name)
					}
					state[name] = fmt.Sprintf("%x/%s/%s/%d/%d", sha256.Sum256(data), identityKey(info), info.Mode(), info.Size(), links)
				}
				return state
			}
			raw := snapshot()
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			next, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if !next.storageReadOnly || next.jobs != nil || next.stamping != nil || next.storageEnrollment != nil || !errors.Is(next.storageProblem, errStorageOwnerRecovery) {
				t.Fatal("unresolved journal did not take the intended early refusal", next.storageProblem)
			}
			refused := next.storageConstruction
			if refused == nil || refused.ref != nil || refused.owner != nil || refused.sources != nil || refused.startupUse != nil {
				t.Fatal("unresolved journal advanced past the original pre-Q gate")
			}
			if !refused.grants["config/claim-complete"] || !refused.grants["config/record-complete"] || refused.grants["anchor/initialize"] || refused.grants["anchor/register"] || refused.grants["anchor/maintenance"] || refused.grants["anchor/census"] {
				t.Fatal("early refusal bypassed original witnesses or mutated Q")
			}
			currentInfo, err := refused.original.config.Stat(".")
			if err != nil || !os.SameFile(originalInfo, currentInfo) {
				t.Fatal("refusal changed original config correspondence", err)
			}
			if !reflect.DeepEqual(raw, snapshot()) {
				t.Fatal("restart changed exact original Q/control/catalog/journal/Ready bytes or identities, losing token/applied/next-sequence conservation")
			}
		})
	}
}

func TestStorageOwnerActualIdentityPrecedenceAndMismatch(t *testing.T) {
	requireStorageLine(t)
	for _, kind := range []string{"retained", "malformed", "unsafe", "persistent-private-mismatch"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			private := filepath.Join(cfg.ProjectDir, startupIdentityDir)
			if err := os.Mkdir(private, 0700); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(cfg.ProjectDir)
			if err != nil {
				t.Fatal(err)
			}
			id := strings.Repeat("9", 64)
			raw := identityRecord{ID: id, Path: cfg.ProjectDir, Folder: identityKey(info)}.line()
			if kind == "malformed" {
				raw = []byte("{ malformed original identity")
			}
			identity := filepath.Join(private, startupIdentityName)
			if err = os.WriteFile(identity, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "unsafe" {
				if err = os.Chmod(identity, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "malformed" || kind == "unsafe" {
				s, err := New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if !s.storageReadOnly || s.storageEnrollment != nil {
					t.Fatal("unreadable original identity registered default workspace")
				}
				if s.storageConstruction == nil || s.storageConstruction.ref == nil {
					t.Fatalf("original identity case did not reach Q inspection: %v", s.storageProblem)
				}
				v, err := s.storageConstruction.ref.cell.q.InspectHost(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if len(v.Workspaces) != 0 || len(v.Roots) != 0 {
					t.Fatal("default hash enrolled before original identity inspection")
				}
				return
			}
			s := ownerActualNew(t, cfg)
			c := s.storageConstruction
			if len(c.ref.cell.ownerCatalog.Workspaces) != 1 || string(c.ref.cell.ownerCatalog.Workspaces[0].OwnerKey) != id || id == digestOf([]byte(cfg.ProjectDir)) {
				t.Fatal("retained startup identity lost precedence")
			}
			if kind == "retained" {
				return
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(private, filepath.Join(cfg.ProjectDir, "original-private")); err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(private, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(identity, raw, 0600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(cfg.DeskConfigDir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			before := ownerReviewDurableState(t, root)
			next, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if !next.storageReadOnly || next.storageEnrollment != nil {
				t.Fatal("same-key foreign persistent private root reattached")
			}
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, root)) {
				t.Fatal("persistent mismatch rewrote old obligations")
			}
		})
	}
}

func TestStorageOwnerRequiredCompletionOutcomes(t *testing.T) {
	requireStorageLine(t)
	points := []storageBootstrapFault{}
	for _, name := range ownerReviewAnchorNames {
		for _, phase := range []string{"sync", "close"} {
			points = append(points, storageBootstrapFault{Name: "anchor/complete-state/" + name, Phase: phase})
		}
	}
	for _, phase := range []string{"parent-sync", "parent-close", "readback"} {
		points = append(points, storageBootstrapFault{Name: "anchor/complete-state", Phase: phase})
	}
	for _, phase := range []string{"sync", "close", "parent-sync", "parent-close", "readback"} {
		points = append(points, storageBootstrapFault{Name: "config/ready-complete", Phase: phase})
	}
	for _, point := range points {
		t.Run(point.Name+"/"+point.Phase, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s := ownerActualNew(t, cfg)
			originalState, err := s.storageConstruction.ref.cell.q.InspectHost(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			_, c, closeCrash := ownerCrashConstruction(t, cfg)
			c.fault = &storageBootstrapFault{Name: point.Name, Phase: point.Phase}
			before := ownerReviewDurableState(t, c.original.config)
			if err := c.loadOrEnrollOwner(t.Context()); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatal("required completion fault was not reached", err)
			}
			if c.grants["anchor/initialize"] || c.grants["anchor/register"] || c.grants["anchor/census"] {
				t.Fatal("completion uncertainty reached reset/register/census")
			}
			// An ordinary reopen of an active catalog selects the exclusive startup
			// census before completion, and that census begins maintenance itself.
			// A completion fault therefore changes no Q state: no maintenance is
			// begun, and every definition, ticket, revision and epoch is the one
			// the previous start published. Catalog, Ready and journal bytes stay
			// exact, and the launch stays read only.
			after := ownerReviewDurableState(t, c.original.config)
			for name, digest := range before {
				if name == storageAnchorName+"/checkpoint.a" || name == storageAnchorName+"/checkpoint.b" {
					continue
				}
				if after[name] != digest {
					t.Fatal("completion rewrote retained source bytes", name)
				}
			}
			if !c.grants["anchor/complete-state"] {
				t.Fatal("completion test did not reach original Q state")
			}
			actual, err := c.ref.cell.q.InspectHost(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if actual.Configuration.Maintenance || actual.Configuration.Revision != originalState.Configuration.Revision || actual.Configuration.Epoch != originalState.Configuration.Epoch || actual.Configuration.Policy != originalState.Configuration.Policy || !reflect.DeepEqual(actual.Roots, originalState.Roots) || !reflect.DeepEqual(actual.Workspaces, originalState.Workspaces) || !reflect.DeepEqual(actual.Configuration.Filesystems, originalState.Configuration.Filesystems) || !reflect.DeepEqual(actual.Configuration.CapacityConstraints, originalState.Configuration.CapacityConstraints) || !reflect.DeepEqual(actual.Slots, originalState.Slots) {
				t.Fatal("a completion fault changed Q state before the census")
			}
			// The next start completes and publishes its census.
			closeCrash()
			ownerActualNew(t, cfg)
		})
	}
}

func TestStorageOwnerActualHomeLeaseContentionPreservesState(t *testing.T) {
	requireStorageLine(t)
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(cfg.DeskConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	shared, err := openHomeLeaseExistingRoot(root, false)
	if err != nil {
		t.Fatal(err)
	}
	before := ownerReviewDurableState(t, root)
	contender, err := New(cfg)
	if err == nil {
		if contender != nil {
			_ = contender.Close()
		}
		t.Fatal("actual New upgraded around another original home SH lifetime")
	}
	if !reflect.DeepEqual(before, ownerReviewDurableState(t, root)) {
		t.Fatal("home contention mutated owner/Q/catalog")
	}
	if err = shared.Close(); err != nil {
		t.Fatal(err)
	}
	again := ownerActualNew(t, cfg)
	if again.storageInfrastructure.home.exclusive {
		t.Fatal("actual successful New did not finish SH handoff")
	}
	if err = again.storageConstruction.ref.cell.current(); err != nil {
		t.Fatal("SH startup lost original currentness", err)
	}
}

// This boundary harness captures the original descriptor across the actual
// phase4 enrollment/census and existing infrastructure conversion. The separate
// actual-New contention test covers constructor refusal and writable restart.
func TestStorageOwnerSameFDHandoffOriginalWitness(t *testing.T) {
	requireStorageLine(t)
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "continuous", true: "changed-name"}[changed], func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s, c, _ := ownerCrashConstruction(t, cfg)
			if err := c.loadOrEnrollOwner(t.Context()); err != nil {
				t.Fatal(err)
			}
			original := s.homeLease
			if !c.home.exclusive || c.home.lease != original {
				t.Fatal("enrollment/census lost original EX descriptor")
			}
			if contender, err := openHomeLeaseExistingRoot(c.original.config, false); err == nil {
				_ = contender.Close()
				t.Fatal("another home SH entered before conversion")
			}
			before := ownerReviewDurableState(t, c.original.config)
			if changed {
				path := filepath.Join(cfg.DeskConfigDir, ".home-use.lock")
				if err := os.Rename(path, filepath.Join(cfg.DeskConfigDir, "original-home-use.lock")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := s.finishInfrastructureStartup(t.Context())
			if changed {
				if err == nil {
					t.Fatal("changed original home name handed off as healthy")
				}
				if !reflect.DeepEqual(before, ownerReviewDurableState(t, c.original.config)) {
					t.Fatal("failed handoff changed Q/catalog")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.homeLease != original || c.home.lease != original || c.home.exclusive {
				t.Fatal("conversion closed/reopened original descriptor")
			}
			e, err := c.forServer(s)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			s.storageEnrollment = e
			if err = s.revalidateStorageBudgetStartup(t.Context()); err != nil {
				t.Fatal("SH handoff failed full owner/Q/capacity revalidation", err)
			}
			contender, err := openHomeLeaseExistingRoot(c.original.config, false)
			if err != nil {
				t.Fatal("SH handoff did not admit another shared lease", err)
			}
			if err = contender.Close(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, c.original.config)) {
				t.Fatal("handoff changed Q/catalog")
			}
		})
	}
}

func TestStorageOwnerActualUnsafeReadyPreserved(t *testing.T) {
	requireStorageLine(t)
	for _, kind := range []string{"directory", "symlink", "hardlink", "public"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s := ownerActualNew(t, cfg)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.DeskConfigDir, storageReadyName)
			switch kind {
			case "directory":
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(storageReadyName+"-original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+"-alias"); err != nil {
					t.Fatal(err)
				}
			case "public":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(cfg.DeskConfigDir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			before := ownerReviewDurableState(t, root)
			next, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if !next.storageReadOnly || next.storageEnrollment != nil || next.jobs != nil {
				t.Fatal("unsafe Ready activated original installation")
			}
			after := ownerReviewDurableState(t, root)
			// This refusal may acquire the explicit ordinary-reopen maintenance fence.
			// It must preserve every catalog, Ready object/byte and outstanding journal.
			for _, name := range []string{"checkpoint.a", "checkpoint.b"} {
				delete(before, storageAnchorName+"/"+name)
				delete(after, storageAnchorName+"/"+name)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unsafe Ready was replaced, reset or rewritten")
			}
		})
	}
}
