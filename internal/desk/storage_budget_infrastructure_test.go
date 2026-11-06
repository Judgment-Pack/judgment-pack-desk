package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func infrastructureTestServer(t *testing.T, config string) *Server {
	t.Helper()
	project, err := OpenProjectRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{project: project, root: project.own.root, configDir: config}
	// The harness supplies the exact New-only launch seal; production has no
	// constructor accepting a caller-selected root, path or callback.
	origin := &storageInfrastructureLaunch{server: s, project: project, configDir: config}
	origin.self = origin
	s.storageInfrastructureLaunch = origin
	t.Cleanup(func() {
		if s.homeLease != nil {
			_ = s.homeLease.Close()
			s.homeLease = nil
		}
		if s.assistant != nil {
			_ = s.assistant.Close()
			s.assistant = nil
		}
		if s.storageInfrastructure != nil {
			_ = s.storageInfrastructure.close()
			s.storageInfrastructure = nil
		}
		_ = project.Close()
	})
	return s
}
func infrastructureTestConfig(t *testing.T, mode os.FileMode, secrets bool) string {
	t.Helper()
	config := filepath.Join(t.TempDir(), "config")
	if err := os.Mkdir(config, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config, mode); err != nil {
		t.Fatal(err)
	}
	if secrets {
		if err := os.Mkdir(filepath.Join(config, secretsDirName), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return config
}
func infrastructureInspectAndGate(t *testing.T, s *Server) *storageInfrastructureStart {
	t.Helper()
	p, err := s.inspectInfrastructure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = p.acquireParentGate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return p
}
func infrastructureRequireNative(t *testing.T, dir string) {
	t.Helper()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = storageAdvisoryReadHeld(context.Background(), f)
	if errors.Is(err, storagebudget.ErrUnsupported) {
		t.Skip("first-provision test requires supported normal-FD filesystem route")
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestStorageInfrastructureRequiresActualLaunchSeal(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, false)
	s := infrastructureTestServer(t, config)
	origin := s.storageInfrastructureLaunch
	s.storageInfrastructureLaunch = nil
	if _, err := s.inspectInfrastructure(context.Background()); err == nil {
		t.Fatal("arbitrary Server minted startup authority")
	}
	copied := *origin
	s.storageInfrastructureLaunch = &copied
	if _, err := s.inspectInfrastructure(context.Background()); err == nil {
		t.Fatal("copied origin minted startup authority")
	}
	s.storageInfrastructureLaunch = origin
	p, err := s.inspectInfrastructure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clone := *p
	if err = clone.acquireParentGate(context.Background()); err == nil {
		t.Fatal("copied start acquired authority")
	}
	if _, err = s.inspectInfrastructure(context.Background()); err == nil {
		t.Fatal("launch seal reused")
	}
}
func TestStorageInfrastructureInspectionDoesNotProvision(t *testing.T) {
	config := infrastructureTestConfig(t, 0755, true)
	if err := os.Chmod(filepath.Join(config, secretsDirName), 0755); err != nil {
		t.Fatal(err)
	}
	s := infrastructureTestServer(t, config)
	p, err := s.inspectInfrastructure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !p.preBudget || p.groups != 0 {
		t.Fatal("inspection issued a provision")
	}
	for _, path := range []string{config, filepath.Join(config, secretsDirName)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0755 {
			t.Fatalf("inspection changed mode: %v", err)
		}
	}
	if _, err = os.Lstat(filepath.Join(config, ".home-use.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection created lease: %v", err)
	}
}
func TestStorageInfrastructureUnsafeSecretsPrecedeLease(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, true)
	if err := os.Chmod(filepath.Join(config, secretsDirName), 0777); err != nil {
		t.Fatal(err)
	}
	s := infrastructureTestServer(t, config)
	_, err := s.inspectInfrastructure(context.Background())
	if _, fallback := storageInfrastructureProblem(err); !fallback {
		t.Fatalf("unsafe secrets lost pack fallback: %v", err)
	}
	if s.storageInfrastructure != nil {
		t.Fatal("failed inspection retained custody")
	}
	if _, err = os.Lstat(filepath.Join(config, ".home-use.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe store created lease: %v", err)
	}
}
func TestStorageInfrastructureOriginalAuthorityMissingLeasePreserved(t *testing.T) {
	config := infrastructureTestConfig(t, 0755, true)
	marker := filepath.Join(config, storageClaimName)
	if err := os.WriteFile(marker, []byte("foreign or partial authority\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, s)
	if p.preBudget {
		t.Fatal("recognized authority selected pre-Q")
	}
	if _, err := p.provisionConfigAndAcquireHome(context.Background()); err == nil {
		t.Fatal("missing original lease replaced")
	}
	info, _ := os.Stat(config)
	if info.Mode().Perm() != 0755 || p.groups != 0 {
		t.Fatal("authority refusal narrowed config or issued provision")
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "foreign or partial authority\n" {
		t.Fatal("authority changed")
	}
	if _, err = os.Lstat(filepath.Join(config, ".home-use.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement lease exists")
	}
}
func TestStorageInfrastructureCatalogBanksDisableFirstProvision(t *testing.T) {
	for _, bank := range []string{"storage-owner.a", "storage-owner.b"} {
		for _, kind := range []string{"foreign-file", "wrong-kind-directory"} {
			t.Run(bank+"/"+kind, func(t *testing.T) {
				config := infrastructureTestConfig(t, 0755, false)
				path := filepath.Join(config, bank)
				if kind == "foreign-file" {
					if err := os.WriteFile(path, []byte("foreign partial catalog\n"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				before, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				server := infrastructureTestServer(t, config)
				p := infrastructureInspectAndGate(t, server)
				if p.preBudget {
					t.Fatal("catalog bank selected first-provision permission")
				}
				if _, err = p.provisionConfigAndAcquireHome(context.Background()); err == nil {
					t.Fatal("catalog state allowed a replacement lease")
				}
				after, err := os.Lstat(path)
				if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
					t.Fatal("catalog object changed")
				}
				if kind == "foreign-file" {
					raw, err := os.ReadFile(path)
					if err != nil || string(raw) != "foreign partial catalog\n" {
						t.Fatal("catalog bytes changed")
					}
				}
				info, err := os.Stat(config)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatal("catalog refusal narrowed config")
				}
				if p.groups != 0 || p.dirs != 0 || p.files != 0 || p.modes != 0 || p.completions != 0 {
					t.Fatal("catalog refusal issued a provision")
				}
				for _, name := range []string{".home-use.lock", secretsDirName} {
					if _, err = os.Lstat(filepath.Join(config, name)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("dependent object created")
					}
				}
			})
		}
	}
}
func TestStorageInfrastructureCatalogBanksRecheckedUnderParentGate(t *testing.T) {
	for _, bank := range []string{"storage-owner.a", "storage-owner.b"} {
		t.Run(bank, func(t *testing.T) {
			config := infrastructureTestConfig(t, 0755, false)
			server := infrastructureTestServer(t, config)
			p, err := server.inspectInfrastructure(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !p.preBudget {
				t.Fatal("test did not begin in positive pre-budget state")
			}
			path := filepath.Join(config, bank)
			if err = os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err = p.acquireParentGate(context.Background()); err == nil {
				t.Fatal("catalog bank missed by parent-gate recheck")
			}
			if p.groups != 0 {
				t.Fatal("stale discovery issued a provision")
			}
			info, err := os.Stat(config)
			if err != nil || info.Mode().Perm() != 0755 {
				t.Fatal("stale discovery narrowed config")
			}
			if _, err = os.Lstat(filepath.Join(config, ".home-use.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stale discovery created lease")
			}
			info, err = os.Lstat(path)
			if err != nil || !info.IsDir() {
				t.Fatal("catalog bank removed")
			}
		})
	}
}
func TestStorageInfrastructureParentGateAndHomeContention(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, true)
	first := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, first)
	second := infrastructureTestServer(t, config)
	q, err := second.inspectInfrastructure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = q.acquireParentGate(context.Background()); err == nil {
		t.Fatal("parent contender acquired EX")
	}
	if q.groups != 0 {
		t.Fatal("contender spent effect allowance")
	}
	if err = p.gate.Close(); err != nil {
		t.Fatal(err)
	}
	p.gate = nil
	if err = os.WriteFile(filepath.Join(config, ".home-use.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	held, err := openHomeLeaseExistingRoot(p.config.root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err = p.acquireParentGate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = p.provisionConfigAndAcquireHome(context.Background()); err == nil {
		t.Fatal("home contender acquired EX")
	}
	if p.groups != 0 {
		t.Fatal("home contender performed completion or mode change")
	}
	if _, err = p.provisionConfigAndAcquireHome(context.Background()); err == nil {
		t.Fatal("failed launch reused provision")
	}
}
func TestStorageInfrastructureLegacyLeaseBytesRemainUnpromoted(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, true)
	data := []byte("legacy lock contents are not a v2 zero-length witness\n")
	if err := os.WriteFile(filepath.Join(config, ".home-use.lock"), data, 0600); err != nil {
		t.Fatal(err)
	}
	// Existing authority selects the unchanged read-only branch, without native
	// physical observation or completion Sync of unknown old file data.
	if err := os.WriteFile(filepath.Join(config, storageRecordName), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	s := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, s)
	h, err := p.provisionConfigAndAcquireHome(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.fixedZeroEligible || p.groups != 0 || !h.exclusive {
		t.Fatal("legacy lease promoted or changed")
	}
	s.assistant, err = p.provisionAssistantStore(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.finishInfrastructureStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(config, ".home-use.lock"))
	if err != nil || string(got) != string(data) {
		t.Fatal("legacy lease bytes changed")
	}
}
func TestStorageInfrastructureEmptyHardlinkedLegacyLeaseRemainsUnpromoted(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, true)
	infrastructureRequireNative(t, config)
	path := filepath.Join(config, ".home-use.lock")
	alias := filepath.Join(config, "legacy-lease-alias")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := linkCount(before); !ok || n != 2 {
		t.Fatal("fixture is not a two-link empty legacy lease")
	}
	server := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, server)
	h, err := p.provisionConfigAndAcquireHome(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.fixedZeroEligible {
		t.Fatal("hardlinked legacy lease promoted")
	}
	// The one completion is config only; no lease completion allowance exists.
	if p.completions != 1 || p.groups != 1 || p.dirs != 0 || p.files != 0 || p.modes != 0 || !p.completed[p.config] {
		t.Fatal("legacy lease received completion provision")
	}
	if err = h.requireFixedZero(context.Background()); err == nil {
		t.Fatal("future v2 eligibility accepted legacy witness")
	}
	if err = p.persistLease(context.Background()); err == nil {
		t.Fatal("ineligible legacy witness entered lease persistence")
	}
	if p.completions != 1 || p.groups != 1 {
		t.Fatal("refused persistence consumed another lease group")
	}
	server.assistant, err = p.provisionAssistantStore(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if p.completions != 2 || p.groups != 2 {
		t.Fatal("expected only config and secrets current persistence")
	}
	if err = server.finishInfrastructureStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, alias} {
		info, err := os.Stat(name)
		if err != nil || !os.SameFile(before, info) || info.Size() != 0 || info.Mode() != before.Mode() {
			t.Fatal("legacy lease name/bytes/mode changed")
		}
		if n, ok := linkCount(info); !ok || n != 2 {
			t.Fatal("legacy hard links changed")
		}
		raw, err := os.ReadFile(name)
		if err != nil || len(raw) != 0 {
			t.Fatal("legacy empty bytes changed")
		}
	}
}
func TestStorageInfrastructureLeaseEligibilityRecheckedBeforePersistence(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, true)
	path := filepath.Join(config, ".home-use.lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	server := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, server)
	lease, err := openHomeLeaseExistingRoot(p.config.root, true)
	if err != nil {
		t.Fatal(err)
	}
	info, err := lease.Stat()
	if err != nil {
		lease.Close()
		t.Fatal(err)
	}
	if err = p.adoptHome(lease, info); err != nil {
		lease.Close()
		t.Fatal(err)
	}
	if !p.home.fixedZeroEligible {
		t.Fatal("fixture not initially zero-length eligible")
	}
	alias := filepath.Join(config, "late-legacy-alias")
	if err = os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if err = p.home.requireFixedZero(context.Background()); err == nil {
		t.Fatal("stale eligible flag accepted changed link count")
	}
	if err = p.persistLease(context.Background()); err == nil {
		t.Fatal("changed eligibility entered persistence")
	}
	if p.groups != 0 {
		t.Fatal("eligibility refusal issued a provision")
	}
	for _, name := range []string{path, alias} {
		after, err := os.Stat(name)
		if err != nil || !os.SameFile(info, after) || after.Size() != 0 {
			t.Fatal("refusal changed legacy lease")
		}
		if n, ok := linkCount(after); !ok || n != 2 {
			t.Fatal("refusal changed legacy links")
		}
	}
}
func TestStorageInfrastructureFailedHomeAdoptionRetainsCallerOwnership(t *testing.T) {
	config := infrastructureTestConfig(t, 0700, true)
	path := filepath.Join(config, ".home-use.lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	server := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, server)
	lease, err := openHomeLeaseExistingRoot(p.config.root, true)
	if err != nil {
		t.Fatal(err)
	}
	info, err := lease.Stat()
	if err != nil {
		lease.Close()
		t.Fatal(err)
	}
	// The primary descriptor remains open but no longer names the launch lease.
	if err = os.Rename(path, filepath.Join(config, "original-lease")); err != nil {
		lease.Close()
		t.Fatal(err)
	}
	if err = os.WriteFile(path, nil, 0600); err != nil {
		lease.Close()
		t.Fatal(err)
	}
	if err = p.adoptHome(lease, info); err == nil {
		lease.Close()
		t.Fatal("changed name adopted")
	}
	if p.home != nil || server.homeLease != nil {
		lease.Close()
		t.Fatal("failed adoption retained close ownership")
	}
	if _, err = lease.Stat(); err != nil {
		t.Fatal("adoptHome closed the caller's descriptor")
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	// The later infrastructure owner teardown has no lease reference to close.
	if err = p.close(); err != nil {
		t.Fatal(err)
	}
	server.storageInfrastructure = nil
}
func TestStorageInfrastructureFreshAndFP1Restart(t *testing.T) {
	parent := t.TempDir()
	infrastructureRequireNative(t, parent)
	config := filepath.Join(parent, "config")
	s := infrastructureTestServer(t, config)
	p := infrastructureInspectAndGate(t, s)
	h, err := p.provisionConfigAndAcquireHome(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	originalFD := h.lease
	s.assistant, err = p.provisionAssistantStore(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.provisionHomeServices(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if p.dirs != 4 || p.files != 1 || p.modes != 0 || p.completions != 0 || p.groups != 5 {
		t.Fatalf("fresh effects: %+v", p)
	}
	if p.gate != nil || h.lease != originalFD || !h.exclusive {
		t.Fatal("home EX transfer lost original descriptor")
	}
	if err = s.finishInfrastructureStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.exclusive || h.lease != originalFD {
		t.Fatal("SH handoff replaced primary lease")
	}
	// A distinct restart does not replay creates. Close the old original lifetime.
	if err = s.homeLease.Close(); err != nil {
		t.Fatal(err)
	}
	s.homeLease = nil
	if err = s.assistant.Close(); err != nil {
		t.Fatal(err)
	}
	s.assistant = nil
	if err = p.close(); err != nil {
		t.Fatal(err)
	}
	s.storageInfrastructure = nil
	restarted := infrastructureTestServer(t, config)
	q := infrastructureInspectAndGate(t, restarted)
	k, err := q.provisionConfigAndAcquireHome(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restarted.assistant, err = q.provisionAssistantStore(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.provisionHomeServices(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if q.dirs != 0 || q.files != 0 || q.modes != 0 || q.completions != 5 || q.groups != 5 {
		t.Fatalf("FP1 effects: dirs=%d files=%d modes=%d completion=%d groups=%d", q.dirs, q.files, q.modes, q.completions, q.groups)
	}
}
func TestStorageInfrastructureCrashCutPreservesCurrentObject(t *testing.T) {
	for _, phase := range []string{"created", "before-persistence", "after-persistence"} {
		t.Run(phase, func(t *testing.T) {
			parent := t.TempDir()
			infrastructureRequireNative(t, parent)
			config := filepath.Join(parent, "config")
			s := infrastructureTestServer(t, config)
			p := infrastructureInspectAndGate(t, s)
			p.fault = &storageInfrastructureFault{name: "config", phase: phase}
			if _, err := p.provisionConfigAndAcquireHome(context.Background()); err == nil {
				t.Fatal("crash cut accepted")
			}
			info, err := os.Stat(config)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatal("created object removed or changed")
			}
			if p.dirs != 1 || p.groups != 1 {
				t.Fatal("attempted provision refunded or replayed")
			}
			if _, err = os.Stat(filepath.Join(config, ".home-use.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("dependent lease effect ran")
			}
			if err = p.close(); err != nil {
				t.Fatal(err)
			}
			s.storageInfrastructure = nil
			restarted := infrastructureTestServer(t, config)
			q := infrastructureInspectAndGate(t, restarted)
			_, err = q.provisionConfigAndAcquireHome(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if q.dirs != 0 || q.files != 1 || q.completions != 1 {
				t.Fatal("fresh launch did not complete current config before lease")
			}
		})
	}
}
func TestStorageInfrastructureFinitePolicyAndHeadroom(t *testing.T) {
	var total storagebudget.CapacityVector
	for _, group := range []struct{ f, d, m uint64 }{{0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {1, 0, 0}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}} {
		v, err := storageAdvisoryEstimate(0, group.f, group.d, group.m)
		if err != nil {
			t.Fatal(err)
		}
		total.Bytes += v.Bytes
		total.Inodes += v.Inodes
	}
	if total.Bytes != 3932160 || total.Inodes != 29 {
		t.Fatalf("finite allowance: %+v", total)
	}
	p := &storageInfrastructureStart{}
	f := storageAdvisoryNativeFacts{available: storagebudget.CapacityVector{Bytes: 1 << 30, Inodes: 4096}, totalBytes: 1 << 30, status: storagePhysicalStatus{Oldest: time.Now(), Inodes: storagePhysicalCounterMeasured}}
	if err := p.checkAvailability(f, total); err != nil {
		t.Fatal(err)
	}
	f.available.Bytes = storageAdvisoryHeadroomMinBytes
	if err := p.checkAvailability(f, total); !errors.Is(err, storagebudget.ErrHeadroom) {
		t.Fatalf("low bytes accepted: %v", err)
	}
	f.available.Bytes = 1 << 30
	f.available.Inodes = storageAdvisoryInodeHeadroomFloor
	if err := p.checkAvailability(f, total); !errors.Is(err, storagebudget.ErrHeadroom) {
		t.Fatalf("low native pool accepted: %v", err)
	}
	f.available.Inodes = 4096
	f.status.Oldest = time.Now().Add(-6 * time.Second)
	if err := p.checkAvailability(f, total); !errors.Is(err, storagebudget.ErrUnknown) {
		t.Fatalf("stale accepted: %v", err)
	}
}
