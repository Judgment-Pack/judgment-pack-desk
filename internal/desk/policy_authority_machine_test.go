package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type policyMachineFixture struct {
	s       *Server
	token   string
	policy  policyReply
	options PolicyAuthorityOptions
}

func TestPolicyMachineCloseDrainsTransactionBeforeLeaseRelease(t *testing.T) {
	f := newPolicyMachineFixture(t)
	f.command(t, "enroll")
	s, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	repository := s.policyRepository()
	entered, release, updated := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		updated <- repository.Update(context.Background(), func(l *policyLedger) (bool, error) {
			close(entered)
			<-release
			l.Recoveries = append(l.Recoveries, policyRecoverySnapshot{SnapshotID: strings.Repeat("a", 64), ArchiveDigest: strings.Repeat("b", 64), LedgerDigest: strings.Repeat("c", 64), At: "2026-10-18T12:00:00Z", Actor: policyActor{Kind: "local", Subject: "fixture"}})
			return true, nil
		})
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	deadline := time.After(time.Second)
	for {
		s.policyAuthorityMachine.mu.Lock()
		closing := s.policyAuthorityMachine.closing
		s.policyAuthorityMachine.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("close did not stop admission")
		case <-time.After(time.Millisecond):
		}
	}
	if err = repository.View(context.Background(), func(policyLedger) error { t.Fatal("close admitted new current operation"); return nil }); err == nil {
		t.Fatal("close accepted new current view")
	}
	if machine, err := openPolicyMachine(f.options, true); err == nil {
		machine.close()
		t.Fatal("exclusive CLI admitted before write drained")
	}
	select {
	case <-closed:
		t.Fatal("Close released in-flight transaction")
	default:
	}
	close(release)
	if err = <-updated; err != nil {
		t.Fatal("admitted write lost custody during close", err)
	}
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	machine, err := openPolicyMachine(f.options, true)
	if err != nil {
		t.Fatal("drained close retained lease", err)
	}
	machine.close()
}

func TestPolicyMachineMissingLedgerAndMoveJournalsRefuse(t *testing.T) {
	for _, kind := range []string{"ledger", "project-journal", "home-journal"} {
		t.Run(kind, func(t *testing.T) {
			f := newPolicyMachineFixture(t)
			switch kind {
			case "ledger":
				if err := os.Remove(filepath.Join(f.s.projectDir, policyDirectory, "ledger.json")); err != nil {
					t.Fatal(err)
				}
			case "project-journal":
				machineWrite(t, f.s.configDir, projectMoveName, []byte(`{}`))
			case "home-journal":
				path := homeJournalPath(legacyConfigDir())
				if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Local CLI uses the normal resolved config; the explicit Config fixture
			// also exercises the independent Server startup journal guard.
			if kind == "project-journal" {
				if s, err := f.configured(t); err == nil {
					s.Close()
					t.Fatal("pending project move started")
				}
				return
			}
			if _, err := PolicyAuthorityLocal(context.Background(), "enroll", f.options, f.s.projectDir); err == nil {
				t.Fatal("enrolled unavailable/pending source")
			}
			if kind == "home-journal" {
				if CheckPolicyAuthorityStartup(&f.options) == nil {
					t.Fatal("automatic home recovery not fenced")
				}
			}
		})
	}
}

func newPolicyMachineFixture(t *testing.T) *policyMachineFixture {
	t.Helper()
	// All CLI/home inspection is confined to this synthetic installation.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("JPACK_DESK_HOME", "")
	s, token := policyTestServer(t)
	stopAuthorityStoreWorker(s)
	created := policyMutation(t, policyCall(s, token, "POST", "/api/policies", policyEditRequest{OperationID: policyOp(t), Title: "Machine custody", Narrative: "Retained machine fixture narrative", ArtifactVersion: "1.0.0"}))
	f := &policyMachineFixture{s: s, token: token, policy: created.Policy, options: PolicyAuthorityOptions{Root: filepath.Join(t.TempDir(), "machine")}}
	status, err := PolicyAuthorityLocal(context.Background(), "init", f.options, s.projectDir)
	if err != nil {
		t.Fatal(err)
	}
	f.options.InstallationID = status.InstallationID
	if _, err = PolicyAuthorityLocal(context.Background(), "classify-legacy", f.options, s.projectDir); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *policyMachineFixture) command(t *testing.T, action string) PolicyAuthorityStatus {
	t.Helper()
	status, err := PolicyAuthorityLocal(context.Background(), action, f.options, f.s.projectDir)
	if err != nil {
		t.Fatal(action, err)
	}
	return status
}
func (f *policyMachineFixture) configured(t *testing.T) (*Server, error) {
	t.Helper()
	cfg := f.s.cfg
	cfg.Root = nil
	cfg.ProjectDir = f.s.projectDir
	cfg.PolicyAuthority = &f.options
	s, err := New(cfg)
	if err == nil {
		stopAuthorityStoreWorker(s)
		t.Cleanup(func() { s.Close() })
	}
	return s, err
}
func machineRaw(t *testing.T, path, name string) []byte {
	t.Helper()
	root, err := openPrivateDataRoot(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, err := readPrivateData(root, name, policyMachineRegistryLimit)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func machineWrite(t *testing.T, path, name string, raw []byte) {
	t.Helper()
	root, err := openPrivateDataRoot(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = writePrivateData(root, name, raw); err != nil {
		t.Fatal(err)
	}
}
func TestPolicyMachineEnrollmentAndCapturedLegacyFence(t *testing.T) {
	f := newPolicyMachineFixture(t)
	legacy, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.policyAuthorityAnchor != nil {
		t.Fatal("legacy acquired authority")
	}
	if _, err = PolicyAuthorityLocal(context.Background(), "enroll", f.options, f.s.projectDir); err == nil {
		t.Fatal("enrolled while configured legacy server owns lease")
	}
	legacy.Close()
	before := machineRaw(t, f.s.projectDir+"/"+policyDirectory, "ledger.json")
	enrolled := f.command(t, "enroll")
	if enrolled.State != "enrolled" || enrolled.WorkspaceID == "" || enrolled.InstallationID != f.options.InstallationID || enrolled.AnchorGeneration != 1 {
		t.Fatal(enrolled)
	}
	if !bytes.Equal(before, machineRaw(t, f.s.projectDir+"/"+policyDirectory, "ledger.json")) {
		t.Fatal("enrollment rewrote ledger")
	}
	if err = f.s.policyRepository().View(context.Background(), func(policyLedger) error { t.Fatal("captured nil admitted current read"); return nil }); err == nil {
		t.Fatal("old server ignored marker")
	}
	if err = f.s.policyRepository().Update(context.Background(), func(*policyLedger) (bool, error) { t.Fatal("captured nil admitted mutation"); return true, nil }); err == nil {
		t.Fatal("old server ignored marker")
	}
	cfg := f.s.cfg
	cfg.Root = nil
	cfg.ProjectDir = f.s.projectDir
	if s, err := New(cfg); err == nil {
		s.Close()
		t.Fatal("omitted machine options accepted demand")
	}
	bound, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	if err = bound.policyRepository().View(context.Background(), func(l policyLedger) error {
		if len(l.Policies) != 1 {
			t.Fatal("history lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = PolicyAuthorityLocal(context.Background(), "classify-legacy", f.options, f.s.projectDir); err == nil {
		t.Fatal("downgraded enrolled mapping")
	}
	bound.Close()
	if _, err = PolicyAuthorityLocal(context.Background(), "enroll", f.options, f.s.projectDir); err == nil {
		t.Fatal("enrollment replay allowed")
	}
	if got := f.command(t, "status"); got.State != "current" {
		t.Fatal(got)
	}
}
func TestPolicyMachineEnrollmentDurableEdges(t *testing.T) {
	for _, edge := range []string{"intent-after", "marker-before", "marker-after", "anchors-directory-before", "anchors-directory-after", "workspace-directory-before", "workspace-directory-after", "anchor-before", "anchor-after", "final-before", "final-after"} {
		t.Run(edge, func(t *testing.T) {
			f := newPolicyMachineFixture(t)
			var phases []string
			hook := func(phase string, root *os.Root, name string, raw []byte) error {
				phases = append(phases, phase)
				if edge == phase+"-before" {
					return errors.New("injected before durable write")
				}
				var err error
				if name == "" {
					err = syncPrivateDirectory(root)
				} else if phase == "marker" {
					err = claimPrivateData(root, name, raw)
				} else {
					err = writePrivateData(root, name, raw)
				}
				if err != nil {
					return err
				}
				if edge == phase+"-after" {
					return errors.New("injected after durable write")
				}
				return nil
			}
			status, err := policyAuthorityLocal(context.Background(), "enroll", f.options, f.s.projectDir, hook)
			var failure *policyFailure
			if !errors.As(err, &failure) || !failure.Uncertain || status.State != "" {
				t.Fatal("uncertainty became success", status, err)
			}
			raw := machineRaw(t, f.options.Root, policyMachineRegistryName)
			r, e := decodePolicyMachineRegistry(raw)
			if e != nil {
				t.Fatal(e)
			}
			want := "enrolling"
			if edge == "final-after" {
				want = "enrolled"
			}
			if r.Workspaces[0].State != want {
				t.Fatal(edge, r.Workspaces[0])
			}
			if _, err = PolicyAuthorityLocal(context.Background(), "enroll", f.options, f.s.projectDir); err == nil {
				t.Fatal("uncertain enrollment replayed")
			}
			opened, err := f.configured(t)
			if want == "enrolling" && err == nil {
				opened.Close()
				t.Fatal("pending workspace started")
			}
			if want == "enrolled" {
				if err != nil {
					t.Fatal(err)
				}
				if got := f.command(t, "status"); got.State != "current" {
					t.Fatal(got)
				}
			}
			if len(phases) > 6 {
				t.Fatal("repair write attempted", phases)
			}
		})
	}
}
func TestPolicyMachineRegistryRollbackCopyAndMissing(t *testing.T) {
	for _, kind := range []string{"legacy-registry", "legacy-without-marker", "missing-registry", "corrupt-registry", "missing-anchor", "copy-project", "replaced-project"} {
		t.Run(kind, func(t *testing.T) {
			f := newPolicyMachineFixture(t)
			legacy := machineRaw(t, f.options.Root, policyMachineRegistryName)
			enrolled := f.command(t, "enroll")
			switch kind {
			case "legacy-registry", "legacy-without-marker":
				machineWrite(t, f.options.Root, policyMachineRegistryName, legacy)
				if kind == "legacy-without-marker" {
					if err := os.Remove(filepath.Join(f.s.projectDir, startupIdentityDir, policyMachineMarkerName)); err != nil {
						t.Fatal(err)
					}
				}
			case "missing-registry":
				if err := os.Remove(filepath.Join(f.options.Root, policyMachineRegistryName)); err != nil {
					t.Fatal(err)
				}
			case "corrupt-registry":
				machineWrite(t, f.options.Root, policyMachineRegistryName, []byte(`{}`))
			case "missing-anchor":
				if err := os.RemoveAll(filepath.Join(f.options.Root, "anchors", enrolled.WorkspaceID)); err != nil {
					t.Fatal(err)
				}
			case "copy-project":
				copyPath := filepath.Join(t.TempDir(), "copy")
				copyPolicyMachineTree(t, f.s.projectDir, copyPath)
				cfg := f.s.cfg
				cfg.Root = nil
				cfg.ProjectDir = copyPath
				cfg.PolicyAuthority = &f.options
				if s, err := New(cfg); err == nil {
					s.Close()
					t.Fatal("portable copy silently attached")
				}
				return
			case "replaced-project":
				old := f.s.projectDir + "-old"
				if err := os.Rename(f.s.projectDir, old); err != nil {
					t.Fatal(err)
				}
				copyPolicyMachineTree(t, old, f.s.projectDir)
			}
			s, err := f.configured(t)
			if kind != "missing-anchor" {
				if err == nil {
					s.Close()
					t.Fatal("lost mapping unbound")
				}
				return
			}
			if err != nil {
				t.Fatal("missing anchor blocked history", err)
			}
			if err = s.policyRepository().View(context.Background(), func(policyLedger) error { t.Fatal("missing anchor current"); return nil }); err == nil {
				t.Fatal("missing anchor allowed")
			}
			if err = s.policyHistoryReader().ReadHistory(context.Background(), func(l policyLedger) error {
				if len(l.Policies) != 1 {
					t.Fatal("history missing")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func copyPolicyMachineTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.Mkdir(to, 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		src, dst := filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())
		if entry.IsDir() {
			copyPolicyMachineTree(t, src, dst)
		} else {
			raw, e := os.ReadFile(src)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(dst, raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
}
func TestPolicyMachineActiveRootAndLeaseCustody(t *testing.T) {
	for _, state := range []string{"legacy", "enrolled"} {
		for _, kind := range []string{"root", "lease", "during-callback"} {
			t.Run(state+"/"+kind, func(t *testing.T) {
				f := newPolicyMachineFixture(t)
				if state == "enrolled" {
					f.command(t, "enroll")
				}
				s, err := f.configured(t)
				if err != nil {
					t.Fatal(err)
				}
				replace := func() {
					if kind == "lease" {
						path := filepath.Join(f.options.Root, policyMachineLeaseName)
						if e := os.Rename(path, path+".old"); e != nil {
							t.Fatal(e)
						}
						if e := os.WriteFile(path, []byte("replacement"), 0600); e != nil {
							t.Fatal(e)
						}
					} else {
						old := f.options.Root + "-old"
						if e := os.Rename(f.options.Root, old); e != nil {
							t.Fatal(e)
						}
						copyPolicyMachineTree(t, old, f.options.Root)
					}
				}
				called := false
				if kind != "during-callback" {
					replace()
				}
				err = s.policyRepository().View(context.Background(), func(policyLedger) error {
					called = true
					if kind == "during-callback" {
						replace()
					}
					return nil
				})
				if err == nil || kind != "during-callback" && called {
					t.Fatal("changed custody admitted current", called, err)
				}
				if err = s.policyRepository().Update(context.Background(), func(*policyLedger) (bool, error) { t.Fatal("changed custody admitted write"); return true, nil }); err == nil {
					t.Fatal("changed custody allowed write")
				}
			})
		}
	}
}
func TestPolicyMachineStrictRegistryAndInitReplay(t *testing.T) {
	f := newPolicyMachineFixture(t)
	raw := machineRaw(t, f.options.Root, policyMachineRegistryName)
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"Version":1`), 1),
		bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(raw, []byte(`"workspaces":[`), []byte(`"workspaces":null,"unexpected":[`), 1),
		bytes.Replace(raw, []byte(`"state":"legacy"`), []byte(`"state":"legacy","intent":null`), 1),
		append(bytes.TrimSuffix(raw, []byte("}")), []byte(`,"unknown":true}`)...),
	} {
		if _, err := decodePolicyMachineRegistry(bad); err == nil {
			t.Fatal("accepted nonclosed registry", string(bad))
		}
	}
	for _, path := range []string{f.options.Root, filepath.Join(t.TempDir(), "empty"), filepath.Join(t.TempDir(), "partial")} {
		if path != f.options.Root {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := PolicyAuthorityLocal(context.Background(), "init", PolicyAuthorityOptions{Root: path}, f.s.projectDir); err == nil {
			t.Fatal("init reused existing leaf")
		}
	}
	wrong := f.options
	wrong.InstallationID = strings.Repeat("f", 64)
	if _, err := PolicyAuthorityLocal(context.Background(), "status", wrong, f.s.projectDir); err == nil {
		t.Fatal("wrong installation accepted")
	}
}
func TestPolicyMachineProtectedRootsAndEarlyMutationGuards(t *testing.T) {
	f := newPolicyMachineFixture(t)
	s, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.moveSnapshot(filepath.Join(t.TempDir(), "move")); err == nil {
		t.Fatal("configured project move allowed")
	}
	if _, err = s.planDeskHome(filepath.Join(t.TempDir(), "home")); err == nil {
		t.Fatal("configured home move allowed")
	}
	before, err := os.ReadDir(s.registryPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	response := policyCall(s, policyTestSession(t, s), "POST", "/api/desks", map[string]string{"name": "Forbidden auto-registration"})
	if response.Code == 201 {
		t.Fatal("browser created configured workspace")
	}
	after, err := os.ReadDir(s.registryPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("refused Desk creation wrote folder")
	}
	for _, target := range []string{f.options.Root, filepath.Dir(f.options.Root), filepath.Join(f.options.Root, "restore")} {
		for _, location := range []dataLocation{{Version: 1, Path: target}, {Version: 1, Path: filepath.Join(t.TempDir(), "safe"), Previous: target}} {
			if store, err := s.openChatDataAt(location, "test", true); err == nil {
				store.root.Close()
				t.Fatal("direct storage selected machine root")
			}
		}
		if s.policyMachineTarget(target) == nil {
			t.Fatal("allowed containment", target)
		}
		if p, _, err := s.policyRecoveryDestination(target); err == nil {
			p.Close()
			t.Fatal("restore entered machine custody")
		}
		var copied bool
		request := httptest.NewRequest("POST", "/api/storage/move", nil)
		recorder := httptest.NewRecorder()
		s.changeChatStorage(recorder, request, moveChatData{Path: target, Revision: "test"}, func(context.Context, *chatDataStore, *chatDataStore, *[]string) error { copied = true; return nil })
		if recorder.Code == 200 || copied {
			t.Fatal("storage entered machine custody")
		}
		policyError(t, recorder, 503, "authority-unavailable")
	}
	for _, protected := range []string{s.configDir, s.projectDir, deskApplicationDataPath()} {
		if policyMachineProtectedPaths(filepath.Join(protected, "authority"), s.configDir, s.project) == nil {
			t.Fatal("machine inside portable root", protected)
		}
	}
	location := dataLocation{Version: 1, Path: filepath.Join(t.TempDir(), "current"), Previous: filepath.Join(t.TempDir(), "previous")}
	raw, _ := json.Marshal(location)
	machineWrite(t, s.configDir, dataLocationName, raw)
	for _, path := range []string{location.Path, location.Previous} {
		if policyMachineProtectedPaths(filepath.Join(path, "authority"), s.configDir, s.project) == nil {
			t.Fatal("machine inside external chat root")
		}
	}
}
