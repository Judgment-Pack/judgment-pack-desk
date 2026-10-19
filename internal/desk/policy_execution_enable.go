package desk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Called only by explicit offline enablement with the exclusive machine-use
// lease. The external executor lease remains continuously held, including while
// its duplicated fd3 is in the nonexecuting store helper. No callback is replayed.
func (m *policyAuthorityMachine) enableExecution(ctx context.Context, project *ProjectRoot, config string, w policyMachineWorkspace) (PolicyAuthorityStatus, error) {
	bad := func() (PolicyAuthorityStatus, error) { return PolicyAuthorityStatus{}, policyMachineRefusal() }
	if w.State != "enrolled" || w.Executor == nil || w.Executor.State != "enrolled" || w.Execution != nil || ctx.Err() != nil || !filepath.IsAbs(m.options.RunnerBin) || filepath.Clean(m.options.RunnerBin) != m.options.RunnerBin {
		return bad()
	}
	location, err := offlineJobsLocation(config, project)
	if err != nil {
		return bad()
	}
	state, err := openPrivateDataRoot(location.Dir, false)
	if err != nil {
		return bad()
	}
	state.Close()
	source, err := pinRunnerExecutable(m.options.RunnerBin)
	if err != nil {
		return bad()
	}
	defer source.close()
	bin, cleanup, err := source.snapshot()
	if err != nil {
		return bad()
	}
	defer cleanup()
	if probeRunnerAdmission(bin) != nil || source.check() != nil {
		return bad()
	}
	ownership, err := m.executorOwnership(w, location.Workspace)
	if err != nil {
		return bad()
	}
	lease, err := openPinnedExecutorLease(ownership)
	if err != nil {
		return bad()
	}
	defer lease.Close()
	if lockPrivateData(lease, true) != nil {
		return bad()
	}
	policies, _, err := openOwnFolder(project.own.root, strings.Split(policyDirectory, "/"), false, "policies")
	if err != nil {
		return bad()
	}
	defer policies.Close()
	ledgerLock, err := policies.Open(".")
	if err != nil {
		return bad()
	}
	defer ledgerLock.Close()
	if waitPrivateDataLock(ctx, ledgerLock, true) != nil {
		return bad()
	}
	raw, ledger, err := readExistingPolicyLedger(policies)
	if err != nil || ledger.SchemaVersion != 2 || ledger.Execution != nil {
		return bad()
	}
	if _, err = policyArchiveClosure(ctx, ledger, retainedArtifactsForProject(project.own.root)); err != nil {
		return bad()
	}
	registryLock, err := m.root.Open(".")
	if err != nil {
		return bad()
	}
	defer registryLock.Close()
	if lockPrivateData(registryLock, true) != nil {
		return bad()
	}
	r, err := m.readRegistry()
	if err != nil {
		return bad()
	}
	current, found, err := policyMachineWorkspaceFor(r, project)
	if err != nil || !found || current.WorkspaceID != w.WorkspaceID || current.Execution != nil || current.Executor == nil || *current.Executor != *w.Executor || r.Generation > policyAuthorityAnchorMaxGeneration-2 {
		return bad()
	}
	marker := &policyMachineMarker{1, r.InstallationID, w.WorkspaceID}
	if policyMachineMarkerMatches(project.own.root, marker) != nil {
		return bad()
	}
	a, err := newPolicyAuthorityAnchor(filepath.Join(m.options.Root, "anchors", w.WorkspaceID), policyAuthorityAnchorIdentity{r.InstallationID, w.WorkspaceID})
	if err != nil {
		return bad()
	}
	a.checkCustody = m.custody
	if m.persistForTest != nil {
		a.persistForTest = func(phase policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
			return m.persistForTest("execution-anchor-"+string(phase), root, policyAuthorityAnchorName, raw)
		}
	}
	a.openRoot = func(create bool) (*os.Root, error) {
		if create || m.custody() != nil {
			return nil, policyMachineRefusal()
		}
		root, _, err := openOwnFolder(m.root, []string{"anchors", w.WorkspaceID}, false, "Policy anchor")
		return root, err
	}
	index := -1
	for i, v := range r.Workspaces {
		if v.WorkspaceID == w.WorkspaceID {
			index = i
		}
	}
	if index < 0 {
		return bad()
	}
	var generation uint64
	var finalDigest string
	err = a.WithCurrent(ctx, raw, func(held *policyAuthorityAnchorHeld) error {
		prepareID, e := executionRandomID()
		if e != nil {
			return policyMachineRefusal()
		}
		executorID, e := executionRandomID()
		if e != nil {
			return policyMachineRefusal()
		}
		r.Version = 3
		r.Generation++
		r.Workspaces[index].Execution = &policyMachineExecution{Version: 1, State: "enabling", Profile: policyExecutionProfile, ExecutorID: executorID, RunnerWorkspace: location.Workspace, Intent: &policyExecutionEnableIntent{prepareID, digestOf(raw)}}
		// Once any transition may have persisted, this object is terminal on
		// failure. No fallible cleanup can establish an absent prior transition.
		complete := false
		defer func() {
			if !complete {
				m.poisoned.Store(true)
			}
		}()
		if ctx.Err() != nil || m.persist("execution-intent", policyMachineRegistryName, m.root, r) != nil {
			return policyMachineRefusal()
		}
		pending := r.Workspaces[index]
		if m.createExecutionDemand(pending) != nil {
			return policyMachineRefusal()
		}
		demand := executionWorkspaceDemand(r.InstallationID, pending)
		if source.check() != nil || m.custody() != nil {
			return policyMachineRefusal()
		}
		if prepareRunnerManualStore(ctx, bin, lease, runnerStorePrepareRequest{1, prepareID, location.Dir, location.Workspace, location.Owner, 3, *ownership, demand}) != nil {
			return policyMachineRefusal()
		}
		if m.custody() != nil || source.check() != nil || ctx.Err() != nil {
			return policyMachineRefusal()
		}
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		if initializePolicyExecutionLedger(&ledger, demand.identity(), now) != nil || validatePolicyLedger(ledger) != nil {
			return policyMachineRefusal()
		}
		next, e := json.Marshal(ledger)
		if e != nil || len(next) > policyLedgerLimit {
			return policyMachineRefusal()
		}
		finalDigest = digestOf(next)
		if held.Advance(ctx, next, func(exact []byte) error {
			if m.persistForTest != nil {
				return m.persistForTest("execution-ledger", policies, "ledger.json", exact)
			}
			return writePrivateData(policies, "ledger.json", exact)
		}) != nil {
			return policyMachineRefusal()
		}
		observation, e := held.Current()
		if e != nil {
			return policyMachineRefusal()
		}
		generation = observation.Generation
		r.Workspaces[index].Execution.State = "enabled"
		r.Workspaces[index].Execution.Intent = nil
		r.Generation++
		if ctx.Err() != nil || m.persist("execution-final", policyMachineRegistryName, m.root, r) != nil || ctx.Err() != nil || m.custody() != nil {
			return policyMachineRefusal()
		}
		complete = true
		return nil
	})
	if err != nil {
		return bad()
	}
	return PolicyAuthorityStatus{Version: 1, InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID, State: "current", LedgerDigest: finalDigest, AnchorGeneration: generation, ExecutorState: "enrolled", ExecutionState: "enabled", ExecutionProfile: policyExecutionProfile}, nil
}
