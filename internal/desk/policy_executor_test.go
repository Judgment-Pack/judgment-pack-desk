package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executorRegistry(t *testing.T, f *policyMachineFixture) policyMachineRegistry {
	t.Helper()
	r, err := decodePolicyMachineRegistry(machineRaw(t, f.options.Root, policyMachineRegistryName))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPolicyExecutorEnrollmentExplicitCurrentAndLease(t *testing.T) {
	f := newPolicyMachineFixture(t)
	if _, err := PolicyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir); err == nil {
		t.Fatal("legacy acquired executor")
	}
	f.command(t, "enroll")
	before := machineRaw(t, f.s.projectDir+"/"+policyDirectory, "ledger.json")
	if r := executorRegistry(t, f); r.Version != 1 || r.Workspaces[0].Executor != nil {
		t.Fatal("storage enrollment changed version", r)
	}
	storage, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PolicyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir); err == nil {
		t.Fatal("running configured Desk allowed enrollment")
	}
	storage.Close()
	status := f.command(t, "enroll-executor")
	if status.State != "current" || status.ExecutorState != "enrolled" {
		t.Fatal(status)
	}
	if !bytes.Equal(before, machineRaw(t, f.s.projectDir+"/"+policyDirectory, "ledger.json")) {
		t.Fatal("executor enrollment rewrote Policy")
	}
	r := executorRegistry(t, f)
	w := r.Workspaces[0]
	if r.Version != 2 || w.Executor == nil || w.Executor.State != "enrolled" || w.Executor.Intent != nil {
		t.Fatal(r)
	}
	if _, err = PolicyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir); err == nil {
		t.Fatal("executor enrollment replayed")
	}
	bound, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	boot, err := bound.policyAuthorityMachine.executorForProject(bound.project, "independent-runner-workspace")
	if err != nil || boot == nil || boot.MachineWorkspace != w.WorkspaceID || boot.RunnerWorkspace != "independent-runner-workspace" || boot.MachineWorkspace == boot.RunnerWorkspace {
		t.Fatal(boot, err)
	}
	if boot.Root != filepath.Join(f.options.Root, "executors", w.WorkspaceID) || boot.RootDevice+":"+boot.RootInode != w.Executor.RootIdentity || boot.FileDevice+":"+boot.FileInode != w.Executor.FileIdentity {
		t.Fatal(boot)
	}
	bound.Close()
	if observed := f.command(t, "status"); observed.State != "current" || observed.ExecutorState != "enrolled" {
		t.Fatal(observed)
	}
}

func TestPolicyExecutorEnrollmentRequiresCurrentAuthority(t *testing.T) {
	for _, edge := range []string{"ledger-missing", "ledger-stale", "anchor-missing"} {
		t.Run(edge, func(t *testing.T) {
			f := newPolicyMachineFixture(t)
			f.command(t, "enroll")
			switch edge {
			case "ledger-missing":
				os.Remove(filepath.Join(f.s.projectDir, policyDirectory, "ledger.json"))
			case "ledger-stale":
				raw := machineRaw(t, f.s.projectDir+"/"+policyDirectory, "ledger.json")
				machineWrite(t, f.s.projectDir+"/"+policyDirectory, "ledger.json", append(raw, '\n'))
			case "anchor-missing":
				w := executorRegistry(t, f).Workspaces[0]
				os.RemoveAll(filepath.Join(f.options.Root, "anchors", w.WorkspaceID))
			}
			if _, err := PolicyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir); err == nil {
				t.Fatal("noncurrent authority enrolled executor")
			}
			if r := executorRegistry(t, f); r.Version != 1 || r.Workspaces[0].Executor != nil {
				t.Fatal("failed validation wrote intent")
			}
			if _, err := os.Lstat(filepath.Join(f.options.Root, "executors")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed validation created custody", err)
			}
		})
	}
}

func TestPolicyExecutorEnrollmentDurableEdges(t *testing.T) {
	for _, edge := range []string{"executor-intent-after", "executors-directory-before", "executors-directory-after", "executor-directory-before", "executor-directory-after", "executor-file-before", "executor-file-after", "executor-final-before", "executor-final-after"} {
		t.Run(edge, func(t *testing.T) {
			f := newPolicyMachineFixture(t)
			f.command(t, "enroll")
			hook := func(phase string, root *os.Root, name string, raw []byte) error {
				if edge == phase+"-before" {
					return errors.New("injected before write")
				}
				var err error
				if name == "" {
					err = syncPrivateDirectory(root)
				} else if phase == "executor-file" {
					err = claimPrivateData(root, name, raw)
				} else {
					err = writePrivateData(root, name, raw)
				}
				if err != nil {
					return err
				}
				if edge == phase+"-after" {
					return errors.New("injected after write")
				}
				return nil
			}
			got, err := policyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir, hook)
			var failure *policyFailure
			if !errors.As(err, &failure) || !failure.Uncertain || got.State != "" {
				t.Fatal("uncertainty became success", got, err)
			}
			status := f.command(t, "status")
			want := "enrolling"
			if edge == "executor-final-after" {
				want = "enrolled"
			}
			if status.ExecutorState != want {
				t.Fatal(status, want)
			}
			if _, err = PolicyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir); err == nil {
				t.Fatal("uncertain executor enrollment replayed")
			}
			server, err := f.configured(t)
			if want == "enrolling" && err == nil {
				server.Close()
				t.Fatal("pending executor started Desk")
			}
			if want == "enrolled" && err != nil {
				t.Fatal("stable final state was not observable", err)
			}
		})
	}
}

func TestPolicyExecutorOrphanDowngradeAndCustody(t *testing.T) {
	for _, edge := range []string{"v1-downgrade", "metadata-removed", "root-missing", "root-replaced", "file-replaced", "hardlink", "mode"} {
		t.Run(edge, func(t *testing.T) {
			f := newPolicyMachineFixture(t)
			f.command(t, "enroll")
			f.command(t, "enroll-executor")
			r := executorRegistry(t, f)
			w := r.Workspaces[0]
			root := filepath.Join(f.options.Root, "executors", w.WorkspaceID)
			leaf := filepath.Join(root, w.WorkspaceID+".lease")
			switch edge {
			case "v1-downgrade", "metadata-removed":
				r.Workspaces[0].Executor = nil
				if edge == "v1-downgrade" {
					r.Version = 1
				}
				raw, _ := json.Marshal(r)
				machineWrite(t, f.options.Root, policyMachineRegistryName, raw)
			case "root-missing":
				os.RemoveAll(root)
			case "root-replaced":
				os.Rename(root, root+"-old")
				os.Mkdir(root, 0700)
				os.WriteFile(leaf, nil, 0600)
				// Keep only a known directory in the external namespace; the
				// replacement is refused by its retained identity, not its name.
				os.RemoveAll(root + "-old")
			case "file-replaced":
				os.Rename(leaf, leaf+"-old")
				os.WriteFile(leaf, nil, 0600)
			case "hardlink":
				if err := os.Link(leaf, filepath.Join(t.TempDir(), "lease-copy")); err != nil {
					t.Fatal(err)
				}
			case "mode":
				os.Chmod(leaf, 0640)
			}
			status, err := PolicyAuthorityLocal(context.Background(), "status", f.options, f.s.projectDir)
			if edge == "v1-downgrade" || edge == "metadata-removed" {
				if err == nil {
					t.Fatal("orphan custody accepted", status)
				}
				if server, err := f.configured(t); err == nil {
					server.Close()
					t.Fatal("downgraded startup")
				}
			} else if err == nil && status.ExecutorState != "unavailable" {
				t.Fatal("custody damage not reported", status)
			}
			if _, err = PolicyAuthorityLocal(context.Background(), "enroll-executor", f.options, f.s.projectDir); err == nil {
				t.Fatal("damaged binding recreated")
			}
		})
	}
}

func TestPolicyExecutorRegistryClosedVersionedExtension(t *testing.T) {
	f := newPolicyMachineFixture(t)
	f.command(t, "enroll")
	before := machineRaw(t, f.options.Root, policyMachineRegistryName)
	f.command(t, "enroll-executor")
	after := machineRaw(t, f.options.Root, policyMachineRegistryName)
	if _, err := decodePolicyMachineRegistry(before); err != nil {
		t.Fatal("legacy registry rejected", err)
	}
	for _, edge := range []string{"v1-executor", "null", "unknown", "state", "root-identity", "missing-file", "duplicate", "pending-null"} {
		t.Run(edge, func(t *testing.T) {
			var object map[string]any
			json.Unmarshal(after, &object)
			w := object["workspaces"].([]any)[0].(map[string]any)
			e := w["executor"].(map[string]any)
			switch edge {
			case "v1-executor":
				object["version"] = 1
			case "null":
				w["executor"] = nil
			case "unknown":
				e["extra"] = true
			case "state":
				e["state"] = "ready"
			case "root-identity":
				e["rootIdentity"] = "01:2"
			case "missing-file":
				delete(e, "fileIdentity")
			case "pending-null":
				w["executor"] = map[string]any{"state": "enrolling", "intent": nil}
			}
			raw, _ := json.Marshal(object)
			if edge == "duplicate" {
				raw = []byte(strings.Replace(string(raw), `"executor":{`, `"executor":null,"executor":{`, 1))
			}
			if _, err := decodePolicyMachineRegistry(raw); err == nil {
				t.Fatal("invalid executor extension accepted", string(raw))
			}
		})
	}
}
