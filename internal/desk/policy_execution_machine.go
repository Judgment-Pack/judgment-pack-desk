package desk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const policyExecutionDemandDirectory = "policy-execution-v1"

// Enabled uniformly with the reviewed Runner boundary for actual isolated
// acceptance. Release requires that gate; there is never an env override.
const policyManualAdmissionAvailable = true

type policyMachineExecution struct {
	Version         int                          `json:"version"`
	State           string                       `json:"state"`
	Profile         string                       `json:"profile"`
	ExecutorID      string                       `json:"executorId"`
	RunnerWorkspace string                       `json:"runnerWorkspace"`
	Intent          *policyExecutionEnableIntent `json:"intent,omitempty"`
}
type policyExecutionEnableIntent struct {
	PrepareID    string `json:"prepareId"`
	LedgerDigest string `json:"ledgerDigest"`
}
type policyExecutionDemand struct {
	Version          int    `json:"version"`
	InstallationID   string `json:"installationId"`
	MachineWorkspace string `json:"machineWorkspace"`
	RunnerWorkspace  string `json:"runnerWorkspace"`
	ExecutorID       string `json:"executorId"`
	Profile          string `json:"profile"`
}

func (d policyExecutionDemand) identity() policyExecutionIdentity {
	return policyExecutionIdentity{d.InstallationID, d.MachineWorkspace, d.RunnerWorkspace, d.ExecutorID}
}
func (d policyExecutionDemand) valid() bool {
	return d.Version == 1 && d.Profile == policyExecutionProfile && d.identity().valid()
}
func executionRandomID() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
func executionWorkspaceDemand(installation string, w policyMachineWorkspace) policyExecutionDemand {
	if w.Execution == nil {
		return policyExecutionDemand{}
	}
	return policyExecutionDemand{1, installation, w.WorkspaceID, w.Execution.RunnerWorkspace, w.Execution.ExecutorID, w.Execution.Profile}
}
func policyMachineExecutionShape(raw []byte, w policyMachineWorkspace) error {
	if w.Execution == nil {
		return nil
	}
	e := w.Execution
	if w.State != "enrolled" || w.Executor == nil || w.Executor.State != "enrolled" || e.Version != 1 || e.Profile != policyExecutionProfile || !retainedArtifactDigest.MatchString(e.ExecutorID) || !executionWorkspace(e.RunnerWorkspace) {
		return policyMachineRefusal()
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	want := []string{"version", "state", "profile", "executorId", "runnerWorkspace"}
	if e.State == "enabling" {
		want = append(want, "intent")
		if e.Intent == nil || !retainedArtifactDigest.MatchString(e.Intent.PrepareID) || !retainedArtifactDigest.MatchString(e.Intent.LedgerDigest) {
			return policyMachineRefusal()
		}
	} else if e.State != "enabled" || e.Intent != nil {
		return policyMachineRefusal()
	}
	if policyAuthorityAnchorObject(fields["execution"], want, "") != nil {
		return policyMachineRefusal()
	}
	if e.Intent != nil {
		var body map[string]json.RawMessage
		_ = json.Unmarshal(fields["execution"], &body)
		if policyAuthorityAnchorObject(body["intent"], []string{"prepareId", "ledgerDigest"}, "") != nil {
			return policyMachineRefusal()
		}
	}
	return nil
}

// Both directions matter: metadata without its fence and a fence hidden by a
// registry rollback both hold. The sibling fixed name makes old Desk refuse.
func (m *policyAuthorityMachine) executionRegistryCustody(r policyMachineRegistry) error {
	expected := map[string]policyExecutionDemand{}
	for _, w := range r.Workspaces {
		if w.Execution != nil {
			expected[w.WorkspaceID] = executionWorkspaceDemand(r.InstallationID, w)
		}
	}
	root, _, err := openOwnFolder(m.root, []string{"executors", policyExecutionDemandDirectory}, false, "execution demands")
	if errors.Is(err, os.ErrNotExist) && len(expected) == 0 {
		return nil
	}
	if err != nil || r.Version != 3 || len(expected) == 0 {
		if root != nil {
			root.Close()
		}
		return policyMachineRefusal()
	}
	defer root.Close()
	entries, err := root.Open(".")
	if err != nil {
		return policyMachineRefusal()
	}
	defer entries.Close()
	names, err := entries.ReadDir(policyMachineWorkspaceLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) || len(names) != len(expected) {
		return policyMachineRefusal()
	}
	for _, entry := range names {
		want, ok := expected[entry.Name()]
		if !ok || !entry.Type().IsRegular() {
			return policyMachineRefusal()
		}
		info, err := root.Lstat(entry.Name())
		if err != nil {
			return policyMachineRefusal()
		}
		links, known := linkCount(info)
		if !known || links != 1 {
			return policyMachineRefusal()
		}
		raw, err := readPrivateData(root, entry.Name(), 4096)
		var demand policyExecutionDemand
		if err != nil || executionDecode(raw, &demand) != nil || !demand.valid() || demand != want {
			return policyMachineRefusal()
		}
		// Reject replacement across the bounded read, even with matching bytes.
		after, err := root.Lstat(entry.Name())
		if err != nil || !os.SameFile(info, after) {
			return policyMachineRefusal()
		}
	}
	return m.custody()
}
func (m *policyAuthorityMachine) createExecutionDemand(w policyMachineWorkspace) error {
	parent, _, err := openOwnFolder(m.root, []string{"executors"}, false, "executor custody")
	if err != nil {
		return policyMachineRefusal()
	}
	defer parent.Close()
	root, _, err := openOwnFolder(parent, []string{policyExecutionDemandDirectory}, false, "execution demands")
	if errors.Is(err, os.ErrNotExist) {
		if parent.Mkdir(policyExecutionDemandDirectory, 0700) != nil || m.syncExecutorDirectory("execution-demands-directory", parent) != nil {
			return policyMachineRefusal()
		}
		root, _, err = openOwnFolder(parent, []string{policyExecutionDemandDirectory}, false, "execution demands")
	}
	if err != nil {
		return policyMachineRefusal()
	}
	defer root.Close()
	demand := executionWorkspaceDemand(m.options.InstallationID, w)
	if !demand.valid() || m.persist("execution-demand", w.WorkspaceID, root, demand) != nil {
		return policyMachineRefusal()
	}
	raw, err := readPrivateData(root, w.WorkspaceID, 4096)
	expected, _ := json.Marshal(demand)
	if err != nil || !bytes.Equal(raw, expected) {
		return policyMachineRefusal()
	}
	return nil
}

// Startup proves an execution demand against current authority. Storage-only
// workspaces retain checked history access without granting current authority.
// Status has its separate diagnostic path and never recursively locks a ledger.
func (m *policyAuthorityMachine) verifyExecutionStartup(p *ProjectRoot, a *policyAuthorityAnchor) error {
	r, err := m.registry()
	if err != nil {
		return err
	}
	w, found, err := policyMachineWorkspaceFor(r, p)
	if err != nil || !found {
		return policyMachineRefusal()
	}
	if w.State == "legacy" {
		return nil
	}
	if a == nil || w.Execution != nil && w.Execution.State != "enabled" {
		return policyMachineRefusal()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	policies, _, err := openOwnFolder(p.own.root, strings.Split(policyDirectory, "/"), false, "policies")
	if err != nil {
		return policyMachineRefusal()
	}
	defer policies.Close()
	lock, err := policies.Open(".")
	if err != nil {
		return policyMachineRefusal()
	}
	defer lock.Close()
	if waitPrivateDataLock(ctx, lock, true) != nil {
		return policyMachineRefusal()
	}
	raw, ledger, err := readExistingPolicyLedger(policies)
	if err != nil {
		return policyMachineRefusal()
	}
	if w.Execution == nil {
		// A missing/unavailable anchor must not prevent schema 2 history reads.
		// Current repository View/Update still prove that captured anchor on
		// every operation; a schema 3 ledger cannot hide its execution demand.
		if ledger.SchemaVersion != 2 || ledger.Execution != nil {
			return policyMachineRefusal()
		}
		return nil
	}
	if a.WithCurrent(ctx, raw, func(*policyAuthorityAnchorHeld) error {
		if ledger.Execution == nil || ledger.Execution.Identity != executionWorkspaceDemand(r.InstallationID, w).identity() {
			return policyMachineRefusal()
		}
		return nil
	}) != nil {
		return policyMachineRefusal()
	}
	return nil
}
