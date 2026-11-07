package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Actual New, its existing isolated offline Runtime fixture and the installed
// native route. No observer, serialized root selector or Healthy callback is
// injected. Unsupported native routes remain an explicit qualification skip.
func ownerActualConfig(t *testing.T) Config {
	t.Helper()
	base := t.TempDir()
	infrastructureRequireNative(t, base)
	return Config{ProjectDir: t.TempDir(), DeskConfigDir: filepath.Join(base, "config"), Token: testToken, Port: testPort, DevMode: true, CodexBin: "off", JpackBin: homeRuntime(t)}
}
func ownerActualNew(t *testing.T, cfg Config) *Server {
	t.Helper()
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
		t.Fatalf("actual supported startup did not become budget ready: %v", s.storageProblem)
	}
	if s.storageInfrastructure.home.exclusive {
		t.Fatal("actual New retained startup EX")
	}
	if err = s.storageConstruction.ref.cell.current(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStorageOwnerActualNewRestartAndAdmission(t *testing.T) {
	cfg := ownerActualConfig(t)
	if err := os.Mkdir(cfg.DeskConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	existing := []byte("retained pre-budget bytes")
	if err := os.WriteFile(filepath.Join(cfg.DeskConfigDir, "existing.json"), existing, 0600); err != nil {
		t.Fatal(err)
	}
	s := ownerActualNew(t, cfg)
	c := s.storageConstruction
	if c.ref.cell.ownerRecord.Version != 2 || c.ref.cell.ownerCatalog.Version != 3 || c.ref.cell.ownerCatalog.Phase != "active" {
		t.Fatal("prototype or incomplete owner activated")
	}
	view, err := c.ref.cell.q.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.Maintenance || len(view.Roots) != 2 || view.Installation[storagebudget.Control].Bytes < 88080384+uint64(len(existing)) {
		t.Fatal("initial provision/catalog/retained bytes missing")
	}
	for _, r := range view.Roots {
		if !r.Known {
			t.Fatal("unmeasured root activated")
		}
	}
	if len(c.ref.cell.ownerCatalog.Workspaces) != 1 || string(c.ref.cell.ownerCatalog.Workspaces[0].OwnerKey) != digestOf([]byte(cfg.ProjectDir)) {
		t.Fatal("positive original identity absence did not retain established fallback")
	}
	if _, err = os.Lstat(filepath.Join(cfg.ProjectDir, startupIdentityDir, startupIdentityName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fresh no-private startup unexpectedly minted identity", err)
	}

	var root *storageRootBinding
	for _, r := range c.owner.roots {
		if r.configControl {
			root = r
		}
	}
	plan := storageSingleFilePlan{Root: root, Target: "owner-checked.json", Stage: ".owner-checked-stage", Workflow: "owner-startup-check", Operation: "first", ExpectedAbsent: true, Bytes: []byte("checked original bytes"), ObjectLimit: 4096}
	a, err := c.owner.admitSingleFile(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Write(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := c.ref.cell.q.InspectHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageRecordName))
	if err != nil {
		t.Fatal(err)
	}
	recordDigest := sha256.Sum256(raw)
	cat := c.ref.cell.ownerCatalog
	record := *c.ref.cell.ownerRecord
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	again := ownerActualNew(t, cfg)
	next := again.storageConstruction
	if *next.ref.cell.ownerRecord != record || !reflect.DeepEqual(next.ref.cell.ownerCatalog, cat) {
		t.Fatal("ordinary restart reminted original definitions")
	}
	after, err := next.ref.cell.q.InspectHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Roots, after.Roots) || !reflect.DeepEqual(before.Workspaces, after.Workspaces) || !reflect.DeepEqual(before.Configuration.Filesystems, after.Configuration.Filesystems) || !reflect.DeepEqual(before.Configuration.CapacityConstraints, after.Configuration.CapacityConstraints) {
		t.Fatal("restart changed immutable Q rows")
	}
	for i, slot := range before.Slots {
		if slot.PersistedApplied != after.Slots[i].PersistedApplied || slot.PersistedNextSequence != after.Slots[i].PersistedNextSequence || !reflect.DeepEqual(slot.Ticket, after.Slots[i].Ticket) {
			t.Fatal("restart lost exact retired obligation", i)
		}
	}
	raw, err = os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageRecordName))
	if err != nil || sha256.Sum256(raw) != recordDigest {
		t.Fatal("restart rewrote immutable record", err)
	}
	got, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "existing.json"))
	if err != nil || !bytes.Equal(got, existing) {
		t.Fatal("pre-budget bytes changed", err)
	}
	// Reopen must permit another real checked write, while conserving the old
	// tuples above. It must not merely return a server in a read-only state.
	plan.Root = nil
	for _, r := range next.owner.roots {
		if r.configControl {
			plan.Root = r
		}
	}
	plan.Target = "owner-checked-after-restart.json"
	plan.Stage = ".owner-checked-after-restart-stage"
	plan.Operation = "second"
	second, err := next.owner.admitSingleFile(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Write(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	if *next.ref.cell.ownerRecord != record || !reflect.DeepEqual(next.ref.cell.ownerCatalog, cat) {
		t.Fatal("second checked write rewrote definitions")
	}
	if next.grants["anchor/initialize"] || next.grants["anchor/register"] {
		t.Fatal("ordinary reopen reset or registered Q")
	}

}

func TestStorageOwnerActualCopiedAuthorityAndCurrentness(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c := s.storageConstruction
	copy := &storageBudgetConstruction{self: c.self, server: c.server, project: c.project, assistant: c.assistant, home: c.home, original: c.original, ref: c.ref, owner: c.owner}
	if _, err := copy.forServer(s); err == nil {
		t.Fatal("copied construction activated")
	}
	e := copiedValue(s.storageEnrollment)
	if err := e.Close(); err == nil {
		t.Fatal("copied enrollment released original custody")
	}
	original := c.ref.cell.record.Config
	c.ref.cell.record.Config.Device++
	c.ref.cell.record.Config.Inode++
	if err := c.ref.cell.current(); err != nil {
		t.Fatal("live device projection was used as persistent authority", err)
	}
	c.ref.cell.record.Config = original
	old := s.assistant
	s.assistant = &assistantStore{}
	if err := c.current(t.Context(), false); err == nil {
		t.Fatal("foreign assistant retained original authority")
	}
	s.assistant = old
	if _, err := c.forServer(&Server{cfg: Config{parent: s}, configDir: s.configDir, assistant: s.assistant}); err == nil {
		t.Fatal("foreign child attached")
	}
}

func TestStorageOwnerActualReadyRefusalsPreserveState(t *testing.T) {
	for _, kind := range []string{"missing", "wrong", "short", "long"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s := ownerActualNew(t, cfg)
			record := *s.storageConstruction.ref.cell.ownerRecord
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.DeskConfigDir, storageReadyName)
			var raw []byte
			switch kind {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "wrong":
				raw = bytes.Repeat([]byte{1}, 32)
			case "short":
				raw = []byte{1}
			case "long":
				raw = bytes.Repeat([]byte{1}, 33)
			}
			if raw != nil {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			next, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if !next.storageReadOnly || next.jobs != nil || next.stamping != nil {
				t.Fatal("unsafe Ready activated workers")
			}
			b, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, storageRecordName))
			if err != nil {
				t.Fatal(err)
			}
			got, err := storageOwnerDecodeRecordV2(b)
			if err != nil || got != record {
				t.Fatal("refusal changed original record", err)
			}
			if kind == "missing" {
				if _, err = os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("deleted post-census Ready recreated")
				}
			} else {
				b, err = os.ReadFile(path)
				if err != nil || !bytes.Equal(b, raw) {
					t.Fatal("bad Ready rewritten", err)
				}
			}
		})
	}
}

func TestStorageOwnerActualPreparedAfterAndConflictingBanks(t *testing.T) {
	for _, kind := range []string{"torn-active-peer", "both-invalid", "conflicting-sequence"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s := ownerActualNew(t, cfg)
			c := s.storageConstruction
			before, err := c.ref.cell.q.InspectHost(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "torn-active-peer":
				if err = os.WriteFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.b"), make([]byte, 8388608), 0600); err != nil {
					t.Fatal(err)
				}
			case "both-invalid":
				for _, n := range []string{"storage-owner.a", "storage-owner.b"} {
					if err = os.WriteFile(filepath.Join(cfg.DeskConfigDir, n), bytes.Repeat([]byte{7}, 8388608), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "conflicting-sequence":
				v, err := storageOwnerDecodeCatalogV3(prepared, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
				if err != nil {
					t.Fatal(err)
				}
				v.Transition[0] ^= 1
				b, err := storageOwnerEncodeCatalogV3(v, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.b"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			next, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if kind == "torn-active-peer" {
				if next.storageReadOnly {
					t.Fatal("actual Q After did not finish original Prepared", next.storageProblem)
				}
				after, err := next.storageConstruction.ref.cell.q.InspectHost(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before.Roots, after.Roots) || after.Configuration.Revision != before.Configuration.Revision+2 {
					t.Fatal("Prepared After repeated Register or lost rows")
				}
			} else {
				if !next.storageReadOnly {
					t.Fatal("conflicting catalogs activated")
				}
				b, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
				if err != nil {
					t.Fatal(err)
				}
				if kind == "both-invalid" && !bytes.Equal(b, bytes.Repeat([]byte{7}, 8388608)) {
					t.Fatal("invalid banks reseeded")
				}
			}
		})
	}
}

func TestStorageOwnerActualPartialQIsNeverInitialized(t *testing.T) {
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.DeskConfigDir, storageAnchorName, "tickets.b")
	if err := os.WriteFile(path, []byte("uncertain"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if !next.storageReadOnly {
		t.Fatal("partial Q activated")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "uncertain" {
		t.Fatal("partial Q reset", err)
	}
}

func TestStorageOwnerActualCloseDrainsUse(t *testing.T) {
	s := ownerActualNew(t, ownerActualConfig(t))
	c, o := s.storageConstruction, s.storageConstruction.owner
	if err := o.retain(); err != nil {
		t.Fatal(err)
	}
	retained := true
	var use *storageInstallationUse
	var mutation *storageMutationAuthority
	var done chan error
	// This cleanup precedes ownerActualNew's Close cleanup even on Fatal. Keep
	// the positive admitted-lifetime assertion; release the actual holds first.
	t.Cleanup(func() {
		if use != nil {
			_ = use.Close()
		}
		if mutation != nil {
			_ = mutation.Close()
		}
		if retained {
			_ = o.drop()
		}
		if done != nil {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	})
	var err error
	use, err = o.ref.acquire()
	if err != nil {
		t.Fatal(err)
	}
	mutation, err = o.beginAdmission(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done = make(chan error, 1)
	go func() { done <- s.Close() }()
	<-o.closingStarted
	select {
	case err := <-done:
		done = nil
		t.Fatal("closed live original use", err)
	default:
	}
	if err = use.Current(); err != nil {
		t.Fatal("closing revoked an admitted lifetime", err)
	}
	if err = c.originCurrent(t.Context(), false); !errors.Is(err, os.ErrClosed) {
		t.Fatal("fresh construction admitted while Closing", err)
	}
	if err = c.ref.cell.current(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("fresh installation currentness bypassed Closing", err)
	}
	if fresh, err := o.ref.acquire(); err == nil {
		_ = fresh.Close()
		t.Fatal("fresh use admitted while Closing")
	}
	if err = mutation.current(t.Context(), true); !errors.Is(err, os.ErrClosed) {
		t.Fatal("retained custody authorized a new mutation", err)
	}
	if fresh, err := o.beginAdmission(t.Context()); err == nil {
		_ = fresh.Close()
		t.Fatal("new ordinary admission while Closing")
	}
	copyUse := *use
	if err = copyUse.Current(); !errors.Is(err, errStorageCustody) {
		t.Fatal("copied use acquired physical custody", err)
	}
	originalAssistant := s.assistant
	s.assistant = &assistantStore{}
	changed := use.Current()
	s.assistant = originalAssistant
	if changed == nil {
		t.Fatal("retained use ignored changed original binding")
	}
	if err = use.Current(); err != nil {
		t.Fatal("original borrower lost currentness after binding restored", err)
	}
	gatePath := filepath.Join(s.configDir, storageUseName)
	if err := os.Rename(gatePath, gatePath+".original-test"); err != nil {
		t.Fatal(err)
	}
	restored := false
	restoreGate := func() {
		if restored {
			return
		}
		_ = os.Remove(gatePath)
		if err := os.Rename(gatePath+".original-test", gatePath); err != nil {
			t.Error(err)
		}
		restored = true
	}
	defer restoreGate()
	if err := os.WriteFile(gatePath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	changed = use.Current()
	restoreGate()
	if changed == nil {
		t.Fatal("retained use accepted a replacement physical gate while Closing")
	}
	if err = use.Current(); err != nil {
		t.Fatal("original gate restoration lost the admitted borrower", err)
	}

	if err = use.Close(); err != nil {
		t.Fatal(err)
	}
	if err = use.Current(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("released use remained current", err)
	}
	if err = mutation.Close(); err != nil {
		t.Fatal(err)
	}
	mutation = nil
	err = o.drop()
	retained = false
	if err != nil {
		t.Fatal(err)
	}
	err = <-done
	done = nil
	if err != nil {
		t.Fatal(err)
	}
	if err = use.Current(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("completed Close revived original use", err)
	}
}

func TestStorageOwnerLogicalLimitIntersectionsAndReadableRefusal(t *testing.T) {
	p := storageHostInitialPolicy()
	var u [storagebudget.Categories]storagebudget.Vector
	u[0] = storagebudget.Vector{Bytes: 1, Files: 262144, Directories: 1}
	u[1] = storagebudget.Vector{Bytes: 1, Files: 1}
	if !errors.Is(storageOwnerUsageWithinLimits(u, p.Installation), storagebudget.ErrLimit) {
		t.Fatal("category ceilings added namespace entitlement")
	}
	u = [storagebudget.Categories]storagebudget.Vector{}
	u[0].Bytes = 6 << 30
	u[1].Bytes = 12 << 30
	u[2].Bytes = 6 << 30
	u[3].Bytes = 1
	if !errors.Is(storageOwnerUsageWithinLimits(u, p.Installation), storagebudget.ErrLimit) {
		t.Fatal("category byte ceilings bypassed shared total")
	}
	u = [storagebudget.Categories]storagebudget.Vector{}
	u[storagebudget.Control].Bytes = 536870913
	if !errors.Is(storageOwnerUsageWithinLimits(u, p.Installation), storagebudget.ErrLimit) {
		t.Fatal("category limit ignored")
	}
	// Refusal uses the existing read routes; it cannot fall through to writers.
	s := ownerActualNew(t, ownerActualConfig(t))
	s.storageReadOnly = true
	for _, tc := range []struct {
		method, path string
		blocked      bool
	}{{"GET", "/api/files", false}, {"GET", "/api/file", false}, {"PUT", "/api/file", true}, {"POST", "/api/assistant/runs", true}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(""))
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		if got := s.serveStorageReadOnly(w, r); got != tc.blocked {
			t.Fatal(tc.path, "read/refusal routing changed")
		}
	}
}

// Existing data is counted before the supported startup hands off home EX.
// Sparse length exercises logical policy without an allocation/workload claim.
func TestStorageOwnerActualOverBudgetPreservesReadableData(t *testing.T) {
	cfg := ownerActualConfig(t)
	privateFixture(t, filepath.Join(cfg.ProjectDir, "saved.md"), "saved project document")
	if err := os.Mkdir(cfg.DeskConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.DeskConfigDir, "large-retained.json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(536870913); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("saved"), 0); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.storageReadOnly || !errors.Is(s.storageProblem, storagebudget.ErrLimit) || s.jobs != nil || s.stamping != nil {
		t.Fatalf("over-budget actual startup did not pause growth: %v", s.storageProblem)
	}
	f, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != 536870913 {
		t.Fatal("over-budget data resized", err)
	}
	raw := make([]byte, 5)
	if _, err = f.Read(raw); err != nil || string(raw) != "saved" {
		t.Fatal("existing bytes unreadable", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/assistant/runs", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	s.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("over-budget startup served growth", w.Code)
	}
	for _, route := range []string{"/api/file?path=saved.md", "/api/storage"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", route, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("refused startup lost safe HTTP read/status", route, w.Code, w.Body.String())
		}
		if route == "/api/file?path=saved.md" {
			var reply struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Content != "saved project document" {
				t.Fatal("HTTP read changed saved document", err)
			}
		} else {
			var reply struct {
				ReadOnly bool                    `json:"readOnly"`
				Usage    *storagebudget.Snapshot `json:"usage"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || !reply.ReadOnly || reply.Usage == nil {
				t.Fatal("HTTP status omitted measured read-only state", err)
			}
		}
	}
}

func TestStorageOwnerActualChildBorrowsInstallation(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(project, ".desk-private", "data"), 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(project, projectConfigName), `{"deskConfigVersion":1}`)
	privateFixture(t, filepath.Join(project, ".desk-private", dataLocationName), `{"version":1,"path":"data"}`)
	marker, err := json.Marshal(dataMarker{Version: 1, Owner: registry.Owner})
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(project, ".desk-private", "data", dataMarkerName), string(marker))
	registry.Desks = append(registry.Desks, entry)
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(cfg.DeskConfigDir, deskHomeRegistryName), string(raw))
	// Both project registrations intentionally retain the same storage owner.
	// Keep each pointer on its own original settings/native relation, including
	// the child's previous history, without pre-seeding startup identities.
	previous := filepath.Join(project, ".desk-private", "previous-data")
	storageTestMust(t, os.Mkdir(previous, 0700))
	privateFixture(t, filepath.Join(previous, dataMarkerName), string(marker))
	privateFixture(t, filepath.Join(project, ".desk-private", dataLocationName), `{"version":1,"path":"data","previous":"previous-data"}`)
	parent := ownerActualNew(t, cfg)
	cat := parent.storageConstruction.ref.cell.ownerCatalog
	expected := map[string]byte{
		filepath.Join(cfg.ProjectDir, ".desk-private", "data"): 1,
		filepath.Join(project, ".desk-private", "data"):        1,
		previous: 2,
	}
	histories, controls := 0, 0
	for _, control := range cat.OwnerControls {
		if control.Role == 2 {
			controls++
		}
	}
	for _, root := range cat.Roots {
		if root.Role != 4 {
			continue
		}
		histories++
		want, ok := expected[string(root.Source.Path)]
		if !ok || root.Source.ControlSelection != want {
			t.Fatal("unexpected, duplicated or reclassified original history")
		}
		delete(expected, string(root.Source.Path))
		if root.Source.ControlKind != 2 || int(root.Source.ControlIndex) >= len(cat.OwnerControls) {
			t.Fatal("history lost its original pointer control")
		}
		control := cat.OwnerControls[root.Source.ControlIndex]
		if int(root.DirectoryObject) >= len(cat.Objects) || int(control.ParentObject) >= len(cat.Objects) || int(cat.Objects[root.DirectoryObject].Parent) >= len(cat.Objects) {
			t.Fatal("history lost its original settings object")
		}
		if string(control.OwnerKey) != registry.Owner || cat.Objects[control.ParentObject].Persistent != cat.Objects[cat.Objects[root.DirectoryObject].Parent].Persistent {
			t.Fatal("shared owner pointer escaped original settings")
		}
		selected, path := control.Location.Selected, control.Location.Path
		if root.Source.ControlSelection == 2 {
			selected, path = control.Location.PreviousSelected, control.Location.Previous
		} else if root.Source.ControlSelection != 1 {
			t.Fatal("history lost current/previous branch")
		}
		if selected != cat.Objects[root.DirectoryObject].Persistent || !bytes.Equal(path, root.Source.Path) {
			t.Fatal("shared owner joined a different path/native")
		}
		originalSettings := false
		for _, source := range parent.storageConstruction.sources.roots {
			if source.role == 3 && source.path == filepath.Dir(string(path)) && source.persistent == cat.Objects[control.ParentObject].Persistent {
				originalSettings = true
			}
		}
		if !originalSettings {
			t.Fatal("pointer parent differs from original discovered settings")
		}
	}
	if controls != 2 || histories != 3 || len(expected) != 0 {
		t.Fatal("shared owner merged original pointers or previous history", controls, histories)
	}
	for _, change := range []string{"settings", "path", "native", "ambiguous"} {
		t.Run(change, func(t *testing.T) {
			bad := ownerCatalogClone(cat)
			var pointers []int
			for i, v := range bad.OwnerControls {
				if v.Role == 2 {
					pointers = append(pointers, i)
				}
			}
			index := pointers[0]
			other := bad.OwnerControls[pointers[1]]
			a := &bad.OwnerControls[index]
			switch change {
			case "settings":
				a.ParentObject = other.ParentObject
			case "path":
				a.Location.Path = append([]byte{}, other.Location.Path...)
			case "native":
				a.Location.Selected = other.Location.Selected
			case "ambiguous":
				bad.OwnerControls = append(bad.OwnerControls, *a)
			}
			// Recompute only the relation digest: changed original custody must
			// still fail the unchanged full catalog validation.
			a = &bad.OwnerControls[index]
			var err error
			a.RelationDigest, err = bad.controlDigest(*a)
			storageTestMust(t, err)
			if bad.validateVersion(*parent.storageConstruction.ref.cell.ownerRecord, parent.storageConstruction.ref.cell.ownerClaim, 3) == nil {
				t.Fatal("mismatched or ambiguous shared-owner pointer accepted")
			}
		})
	}
	child := parent.desks[entry.ID]
	if child == nil || child.storageReadOnly || child.storageEnrollment == nil {
		t.Fatal("actual registered child did not enroll")
	}
	if child.storageConstruction != nil || child.storageEnrollment.construction != parent.storageConstruction || child.storageEnrollment.ref.cell != parent.storageEnrollment.ref.cell {
		t.Fatal("child minted independent installation")
	}
	if err = child.Close(); err != nil {
		t.Fatal(err)
	}
	if err = parent.storageConstruction.ref.cell.current(); err != nil {
		t.Fatal("child Close revoked parent original custody", err)
	}
	if pins := parent.storageStartupProviders; pins != nil {
		if err = pins.check(t.Context()); err != nil {
			t.Fatal("child closed parent provider pin", err)
		}
	}
}

// A crash-boundary harness stops between steps of the same original launch.
// Actual New/restart tests above independently exercise the public constructor.
func ownerCrashConstruction(t *testing.T, cfg Config) (*Server, *storageBudgetConstruction, func()) {
	t.Helper()
	return ownerCrashConstructionCloseCheck(t, cfg, func(err error) { t.Error(err) })
}
func ownerCrashConstructionCloseCheck(t *testing.T, cfg Config, closeCheck func(error)) (*Server, *storageBudgetConstruction, func()) {
	return ownerCrashConstructionStart(t, cfg, closeCheck, true)
}
func ownerCrashConstructionStart(t *testing.T, cfg Config, closeCheck func(error), prepare bool) (*Server, *storageBudgetConstruction, func()) {
	t.Helper()
	project, err := OpenProjectRoot(cfg.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, project: project, root: project.own.root, projectDir: project.dir, configDir: cfg.DeskConfigDir}
	var c *storageBudgetConstruction
	closed := false
	close := func() {
		if closed {
			return
		}
		closed = true
		if !prepare {
			c = s.storageConstruction
			if s.storageEnrollment != nil {
				if e := s.storageEnrollment.Close(); e != nil {
					closeCheck(e)
				}
				s.storageEnrollment = nil
			}
		}
		if c != nil {
			if err := c.Close(); err != nil {
				closeCheck(err)
			}
		}
		if s.homeLease != nil {
			if err := s.homeLease.Close(); err != nil {
				t.Error(err)
			}
			s.homeLease = nil
		}
		if s.assistant != nil {
			if err := s.assistant.Close(); err != nil {
				t.Error(err)
			}
			s.assistant = nil
		}
		if s.storageInfrastructure != nil {
			if err := s.storageInfrastructure.close(); err != nil {
				t.Error(err)
			}
			s.storageInfrastructure = nil
		}
		if err := project.own.close(); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(close)
	home, err := readDeskHome(cfg.DeskConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	s.home = home
	origin := &storageInfrastructureLaunch{server: s, project: project, configDir: s.configDir}
	origin.self = origin
	s.storageInfrastructureLaunch = origin
	project.detach()
	p := infrastructureInspectAndGate(t, s)
	h, err := p.provisionConfigAndAcquireHome(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.assistant, err = p.provisionAssistantStore(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	if home != nil {
		if err = p.provisionHomeServices(t.Context(), h); err != nil {
			t.Fatal(err)
		}
	}
	if prepare {
		c, err = s.prepareStorageBudgetConstruction(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	return s, c, close
}

func TestStorageOwnerPreparedBeforeUsesExactRevision(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact-before", true: "stale-before"}[stale], func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s, c, close := ownerCrashConstruction(t, cfg)
			cfg.ProjectDir = s.projectDir
			c.fault = &storageBootstrapFault{Name: "anchor/register", Phase: "before-effect"}
			if err := c.loadOrEnrollOwner(t.Context()); err == nil {
				t.Fatal("Register fault not reached")
			}
			raw, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
			if err != nil {
				t.Fatal(err)
			}
			cat, err := storageOwnerDecodeCatalogV3(raw, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
			if err != nil {
				t.Fatal(err)
			}
			v, err := c.ref.cell.q.InspectHost(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if cat.Phase != "prepared" || cat.ExpectedQRevision != 1 || v.Configuration.Revision != 1 || len(v.Roots) != 0 {
				t.Fatal("fault crossed Prepared-before boundary")
			}
			if stale {
				if _, err = c.ref.cell.q.BeginMaintenance(t.Context(), 1); err != nil {
					t.Fatal(err)
				}
			}
			close()
			next, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if stale {
				if !next.storageReadOnly {
					t.Fatal("stale Before was rebased")
				}
				got, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
				if err != nil || !bytes.Equal(got, raw) {
					t.Fatal("stale Prepared rewritten", err)
				}
			} else {
				if next.storageReadOnly {
					t.Fatal("exact Before did not continue", next.storageProblem)
				}
				active := next.storageConstruction.ref.cell.ownerCatalog
				if active.Transition != cat.Transition || !reflect.DeepEqual(active.Roots, cat.Roots) || !reflect.DeepEqual(active.Filesystems, cat.Filesystems) {
					t.Fatal("continuation reminted original rows")
				}
			}
		})
	}
}

func TestStorageOwnerCreationUncertaintyNeverRetries(t *testing.T) {
	for _, control := range []struct{ key, name string }{{"config/claim", storageClaimName}, {"config/record", storageRecordName}, {"config/ready", storageReadyName}} {
		for _, phase := range []string{"before-effect", "write", "sync", "close", "parent-sync", "readback"} {
			t.Run(control.key+"/"+phase, func(t *testing.T) {
				cfg := ownerActualConfig(t)
				_, c, _ := ownerCrashConstruction(t, cfg)
				c.fault = &storageBootstrapFault{Name: control.key, Phase: phase}
				if err := c.loadOrEnrollOwner(t.Context()); err == nil {
					t.Fatal("control fault not reached")
				}
				if !c.grants[control.key] {
					t.Fatal("uncertain grant was reusable")
				}
				if err := c.reserveOwnerEffect(t.Context(), c.original.config, nil, control.key); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("failed grant reused", err)
				}
				absent := []string{}
				switch control.key {
				case "config/claim":
					absent = []string{storageRecordName, storageAnchorName, storageReadyName}
				case "config/record":
					absent = []string{storageReadyName, storageAnchorName + "/checkpoint.a", storageAnchorName + "/checkpoint.b", storageAnchorName + "/tickets.a", storageAnchorName + "/tickets.b", storageAnchorName + "/manifests"}
				case "config/ready":
					v, err := c.ref.cell.q.Snapshot(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					if !v.Maintenance {
						t.Fatal("uncertain Ready published census")
					}
					for _, r := range v.Roots {
						if r.Known {
							t.Fatal("uncertain Ready made roots known")
						}
					}
				}
				for _, name := range absent {
					if _, err := os.Lstat(filepath.Join(cfg.DeskConfigDir, name)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("dependent growth followed uncertain control", name, err)
					}
				}
				info, err := os.Lstat(filepath.Join(cfg.DeskConfigDir, control.name))
				if phase == "before-effect" {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatal("before-effect created control")
					}
				} else if err != nil || !info.Mode().IsRegular() {
					t.Fatal("uncertain control was removed", err)
				}
			})
		}
	}
}

func TestStorageOwnerCatalogCutsRetainExactTransaction(t *testing.T) {
	for _, key := range []string{"config/prepared", "config/active"} {
		for _, phase := range []string{"write", "sync", "close", "readback"} {
			t.Run(key+"/"+phase, func(t *testing.T) {
				cfg := ownerActualConfig(t)
				s, c, close := ownerCrashConstruction(t, cfg)
				cfg.ProjectDir = s.projectDir
				c.fault = &storageBootstrapFault{Name: key, Phase: phase}
				if err := c.loadOrEnrollOwner(t.Context()); err == nil {
					t.Fatal("catalog fault not reached")
				}
				record := *c.ref.cell.ownerRecord
				claim := c.ref.cell.ownerClaim
				prepared, err := os.ReadFile(filepath.Join(cfg.DeskConfigDir, "storage-owner.a"))
				if err != nil {
					t.Fatal(err)
				}
				cat, decoded := storageOwnerDecodeCatalogV3(prepared, record, claim)
				v, err := c.ref.cell.q.InspectHost(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if key == "config/prepared" && (v.Configuration.Revision != 1 || len(v.Roots) != 0) {
					t.Fatal("Prepared failure reached Register")
				}
				if key == "config/active" && (v.Configuration.Revision != 2 || len(v.Roots) == 0) {
					t.Fatal("Active failure lost actual Q After")
				}
				if _, err = os.Lstat(filepath.Join(cfg.DeskConfigDir, storageReadyName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("catalog uncertainty published Ready")
				}
				close()
				next := ownerActualNew(t, cfg)
				if *next.storageConstruction.ref.cell.ownerRecord != record {
					t.Fatal("catalog continuation reminted record")
				}
				if decoded == nil {
					active := next.storageConstruction.ref.cell.ownerCatalog
					if active.Transition != cat.Transition || !reflect.DeepEqual(active.Roots, cat.Roots) || !reflect.DeepEqual(active.Constraints, cat.Constraints) {
						t.Fatal("durable Prepared definitions changed during continuation")
					}
				}
			})
		}
	}
}

func TestStorageOwnerFixedControlCoverage(t *testing.T) {
	cfg := ownerActualConfig(t)
	_, c, _ := ownerCrashConstruction(t, cfg)
	if err := c.loadOrEnrollOwner(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := c.ref.cell.q.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Installation[storagebudget.Control].Bytes != 88080384 {
		t.Fatal("68 MiB provision plus 16 MiB banks changed or Ready double counted", v.Installation[storagebudget.Control])
	}
	fixed := map[string]int64{
		".home-use.lock": 0, storageBootstrapName: 0, storageUseName: 0, storageClaimName: 4096, storageRecordName: 4096, storageIntentName: 2097152,
		"storage-owner.a": 8388608, "storage-owner.b": 8388608, storageReadyName: 32, ".data.lock": 0, ".data-work.lock": 0,
		storageAnchorName + "/lock": 0, storageAnchorName + "/checkpoint.a": 4194304, storageAnchorName + "/checkpoint.b": 4194304,
		storageAnchorName + "/tickets.a": 1048576, storageAnchorName + "/tickets.b": 1048576, storageAnchorName + "/manifests": 33554432,
	}
	var bytes int64
	for name, size := range fixed {
		info, err := os.Stat(filepath.Join(cfg.DeskConfigDir, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() != size {
			t.Fatal("fixed retained inventory changed", name, err)
		}
		bytes += size
	}
	if len(fixed) != 17 || bytes != 62922784 {
		t.Fatal("fixed retained source arithmetic changed", len(fixed), bytes)
	}
}
