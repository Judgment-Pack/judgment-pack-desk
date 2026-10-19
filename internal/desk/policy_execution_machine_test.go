package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const runnerAdmissionReport = `{"version":3,"capabilities":["jobs/1","required-boot-capabilities/1","executor-ownership/1","policy-manual-pack-run/1"],"governanceAdmission":true,"governanceProfiles":["local-admin-manual-pack/1"]}`

func admissionPrepareFixture(t *testing.T, delivery string) (*policyMachineFixture, string) {
	t.Helper()
	f := newPolicyMachineFixture(t)
	f.command(t, "enroll")
	f.command(t, "enroll-executor")
	location, err := resolveJobsLocation(f.s.configDir, f.s.projectDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := openPrivateDataRoot(location.Dir, true)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "runner")
	// This stub verifies the real inherited shared flock and exact helper reply
	// binding. It does not stand in for SQLite migration or the actual gate.
	code := `import sys,json,hashlib,fcntl,os
line=sys.stdin.buffer.readline()
assert line.endswith(b'\n') and sys.stdin.buffer.read()==b''
q=json.loads(line)
assert q['leaseFd']==3 and q['workspace']==q['executionDemand']['runnerWorkspace']
fcntl.flock(3,fcntl.LOCK_EX|fcntl.LOCK_NB)
path=q['executorOwnership']['root']+'/'+q['executorOwnership']['machineWorkspace']+'.lease'
fd=os.open(path,os.O_RDONLY)
try:
 fcntl.flock(fd,fcntl.LOCK_SH|fcntl.LOCK_NB)
 raise AssertionError('parent did not retain EX lease')
except BlockingIOError: pass
os.close(fd)
print(json.dumps({'version':1,'status':'ok','prepareId':q['prepareId'],'requestDigest':'sha256:'+hashlib.sha256(line[:-1]).hexdigest(),'delivery':` + fmtJSONString(delivery) + `,'storeSchema':'4'},separators=(',',':')))
`
	script := "#!/bin/sh\nif [ \"$1\" = capabilities ]; then printf '%s\\n' " + executorShellQuote(runnerAdmissionReport) + "; exit; fi\nif [ \"$1\" != prepare-manual-store ]; then exit 30; fi\nexec " + executorShellQuote(python) + " -c " + executorShellQuote(code) + "\n"
	if err = os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f.options.RunnerBin = bin
	return f, location.Dir
}
func fmtJSONString(value string) string { raw, _ := json.Marshal(value); return string(raw) }
func enableAdmissionFixture(t *testing.T, f *policyMachineFixture, hook func(string, *os.Root, string, []byte) error) (PolicyAuthorityStatus, error) {
	t.Helper()
	m, err := openPolicyMachine(f.options, true)
	if err != nil {
		return PolicyAuthorityStatus{}, err
	}
	defer m.close()
	m.persistForTest = hook
	r, err := m.registry()
	if err != nil {
		return PolicyAuthorityStatus{}, err
	}
	w, ok, err := policyMachineWorkspaceFor(r, f.s.project)
	if err != nil || !ok {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	return m.enableExecution(context.Background(), f.s.project, f.s.configDir, w)
}
func executionMachineWrite(phase string, root *os.Root, name string, raw []byte) error {
	if name == "" {
		return syncPrivateDirectory(root)
	}
	if phase == "execution-demand" {
		return claimPrivateData(root, name, raw)
	}
	return writePrivateData(root, name, raw)
}
func TestPolicyExecutionOfflineTransitionAndStickyFence(t *testing.T) {
	f, _ := admissionPrepareFixture(t, "committed")
	before := machineRaw(t, f.options.Root, policyMachineRegistryName)
	status, err := enableAdmissionFixture(t, f, nil)
	if err != nil || status.ExecutionState != "enabled" || status.State != "current" {
		t.Fatal(status, err)
	}
	r := executorRegistry(t, f)
	w := r.Workspaces[0]
	if r.Version != 3 || w.Execution == nil || w.Execution.State != "enabled" || w.Execution.Intent != nil {
		t.Fatal(r)
	}
	if _, err = enableAdmissionFixture(t, f, nil); err == nil {
		t.Fatal("enable replayed")
	}
	s, err := f.configured(t)
	if err != nil {
		t.Fatal("completed transition did not restart", err)
	}
	s.Close()
	// The registry alone cannot downgrade the external immutable demand.
	machineWrite(t, f.options.Root, policyMachineRegistryName, before)
	if _, err = PolicyAuthorityLocal(context.Background(), "status", f.options, ""); err == nil {
		t.Fatal("v2 rollback hid external demand")
	}
	if s, err = f.configured(t); err == nil {
		s.Close()
		t.Fatal("v2 rollback started")
	}
	fence := filepath.Join(f.options.Root, "executors", policyExecutionDemandDirectory, w.WorkspaceID)
	if _, err = os.Lstat(fence); err != nil {
		t.Fatal("rollback erased fence", err)
	}
}
func TestPolicyExecutionOfflineDurableFaultsHoldWithoutRepair(t *testing.T) {
	for _, edge := range []string{"execution-intent-after", "execution-demands-directory-before", "execution-demands-directory-after", "execution-demand-before", "execution-demand-after", "execution-anchor-intent-before", "execution-anchor-intent-after", "execution-ledger-before", "execution-ledger-after", "execution-anchor-final-before", "execution-anchor-final-after", "execution-final-before", "execution-final-after", "helper-recovered"} {
		t.Run(edge, func(t *testing.T) {
			delivery := "committed"
			if edge == "helper-recovered" {
				delivery = "recovered"
			}
			f, _ := admissionPrepareFixture(t, delivery)
			hit := false
			hook := func(phase string, root *os.Root, name string, raw []byte) error {
				if edge == phase+"-before" {
					hit = true
					return errors.New("injected before")
				}
				if err := executionMachineWrite(phase, root, name, raw); err != nil {
					return err
				}
				if edge == phase+"-after" {
					hit = true
					return errors.New("injected after")
				}
				return nil
			}
			status, err := enableAdmissionFixture(t, f, hook)
			if err == nil || status.State != "" {
				t.Fatal("uncertain transition became success", status, err)
			}
			if edge != "helper-recovered" && !hit {
				t.Fatal("fault edge not reached")
			}
			before := machineRaw(t, f.options.Root, policyMachineRegistryName)
			if _, err = enableAdmissionFixture(t, f, nil); err == nil {
				t.Fatal("partial transition replayed")
			}
			if s, err := f.configured(t); err == nil {
				s.Close()
				if edge != "execution-final-after" {
					t.Fatal("partial transition started")
				}
			} else if edge == "execution-final-after" {
				t.Fatal("complete durable transition was not observable", err)
			}
			if !bytes.Equal(before, machineRaw(t, f.options.Root, policyMachineRegistryName)) {
				t.Fatal("lookup repaired partial transition")
			}
		})
	}
}
func TestPolicyExecutionRegistryFenceClosureAndSchema(t *testing.T) {
	f, _ := admissionPrepareFixture(t, "committed")
	if _, err := enableAdmissionFixture(t, f, nil); err != nil {
		t.Fatal(err)
	}
	original := machineRaw(t, f.options.Root, policyMachineRegistryName)
	r := executorRegistry(t, f)
	w := r.Workspaces[0]
	path := filepath.Join(f.options.Root, "executors", policyExecutionDemandDirectory)
	demand := machineRaw(t, path, w.WorkspaceID)
	for _, edge := range []string{"missing", "orphan", "empty", "changed", "null", "unknown", "wrong-version"} {
		t.Run(edge, func(t *testing.T) {
			switch edge {
			case "missing":
				os.Remove(filepath.Join(path, w.WorkspaceID))
			case "orphan":
				os.WriteFile(filepath.Join(path, strings.Repeat("f", 64)), demand, 0600)
			case "empty":
				machineWrite(t, path, w.WorkspaceID, []byte(`{}`))
			case "changed":
				machineWrite(t, path, w.WorkspaceID, bytes.Replace(demand, []byte(w.Execution.ExecutorID), []byte(strings.Repeat("e", 64)), 1))
			case "null":
				machineWrite(t, f.options.Root, policyMachineRegistryName, bytes.Replace(original, []byte(`"execution":{`), []byte(`"execution":null,"unused":{`), 1))
			case "unknown":
				machineWrite(t, f.options.Root, policyMachineRegistryName, bytes.Replace(original, []byte(`"execution":{`), []byte(`"execution":{"unknown":true,`), 1))
			case "wrong-version":
				machineWrite(t, f.options.Root, policyMachineRegistryName, bytes.Replace(original, []byte(`"version":3`), []byte(`"version":2`), 1))
			}
			if m, err := openPolicyMachine(f.options, false); err == nil {
				m.close()
				t.Fatal("bad closure accepted")
			}
			os.Remove(filepath.Join(path, strings.Repeat("f", 64)))
			machineWrite(t, path, w.WorkspaceID, demand)
			machineWrite(t, f.options.Root, policyMachineRegistryName, original)
		})
	}
}
func TestJobsAdmissionProbeClosedAndOwnershipCompatibility(t *testing.T) {
	for _, row := range []struct {
		name, report  string
		manual, owned bool
	}{
		{"v2", runnerOwnershipReport, false, true}, {"v3", runnerAdmissionReport, true, true},
		{"unknown-profile", strings.Replace(runnerAdmissionReport, policyExecutionProfile, "other/1", 1), false, false},
		{"incomplete", strings.Replace(runnerAdmissionReport, `,"governanceProfiles":["local-admin-manual-pack/1"]`, "", 1), false, false},
		{"false", strings.Replace(runnerAdmissionReport, `true`, `false`, 1), false, false},
		{"extra", strings.Replace(runnerAdmissionReport, `"version":3`, `"extra":0,"version":3`, 1), false, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			j, _ := ownedTestCompanion(t, "printf '%s\\n' "+executorShellQuote(row.report), "exit 1")
			if (probeRunnerAdmission(j.bin) == nil) != row.manual || (probeRunnerOwnership(j.bin) == nil) != row.owned {
				t.Fatal("wrong capability admission", row)
			}
		})
	}
}
