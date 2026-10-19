package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Executor custody is an explicit extension of the machine mapping. Pending
// state never supplies a launch binding, even when the file already exists.
type policyMachineExecutor struct {
	State        string               `json:"state"`
	Intent       *policyMachineIntent `json:"intent,omitempty"`
	RootIdentity string               `json:"rootIdentity,omitempty"`
	FileIdentity string               `json:"fileIdentity,omitempty"`
}

// runnerExecutorOwnership is trusted boot configuration. Machine and Runner
// workspace identities have independent origins and must never be conflated.
type runnerExecutorOwnership struct {
	Version          int    `json:"version"`
	Root             string `json:"root"`
	MachineWorkspace string `json:"machineWorkspace"`
	RunnerWorkspace  string `json:"runnerWorkspace"`
	RootDevice       string `json:"rootDevice"`
	RootInode        string `json:"rootInode"`
	FileDevice       string `json:"fileDevice"`
	FileInode        string `json:"fileInode"`
}

func policyMachineWorkspaceShape(raw []byte, version int) error {
	fields := []string{"workspaceId", "projectPath", "projectIdentity", "state", "intent"}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return policyMachineRefusal()
	}
	if version >= 2 && object["executor"] != nil {
		fields = append(fields, "executor")
	}
	if version == 3 && object["execution"] != nil {
		fields = append(fields, "execution")
	}
	return policyAuthorityAnchorObject(raw, fields, "intent")
}

func executorIdentityParts(identity string) (string, string, bool) {
	parts := strings.Split(identity, ":")
	if len(parts) != 2 {
		return "", "", false
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != p || i == 1 && n == 0 {
			return "", "", false
		}
	}
	return parts[0], parts[1], true
}

func policyMachineExecutorShape(raw []byte, w policyMachineWorkspace) error {
	if w.Executor == nil {
		return nil
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(raw, &object)
	e := w.Executor
	if w.State != "enrolled" {
		return policyMachineRefusal()
	}
	switch e.State {
	case "enrolling":
		if policyAuthorityAnchorObject(object["executor"], []string{"state", "intent"}, "") != nil || e.Intent == nil || !policyAuthorityAnchorNonceForm.MatchString(e.Intent.Nonce) || !retainedArtifactDigest.MatchString(e.Intent.LedgerDigest) {
			return policyMachineRefusal()
		}
		var pending map[string]json.RawMessage
		_ = json.Unmarshal(object["executor"], &pending)
		if policyAuthorityAnchorObject(pending["intent"], []string{"nonce", "ledgerDigest"}, "") != nil {
			return policyMachineRefusal()
		}
	case "enrolled":
		_, _, rootOK := executorIdentityParts(e.RootIdentity)
		_, _, fileOK := executorIdentityParts(e.FileIdentity)
		if policyAuthorityAnchorObject(object["executor"], []string{"state", "rootIdentity", "fileIdentity"}, "") != nil || !rootOK || !fileOK {
			return policyMachineRefusal()
		}
	default:
		return policyMachineRefusal()
	}
	return nil
}

// An orphan directory holds the entire profile. Dropping executor metadata or
// restoring a v1 registry cannot silently hide already established custody.
func (m *policyAuthorityMachine) executorRegistryCustody(r policyMachineRegistry) error {
	executors, _, err := openOwnFolder(m.root, []string{"executors"}, false, "executor leases")
	if errors.Is(err, os.ErrNotExist) {
		return m.executionRegistryCustody(r)
	}
	if err != nil || r.Version < 2 {
		if executors != nil {
			executors.Close()
		}
		return policyMachineRefusal()
	}
	defer executors.Close()
	entries, err := executors.Open(".")
	if err != nil {
		return policyMachineRefusal()
	}
	defer entries.Close()
	names, err := entries.ReadDir(policyMachineWorkspaceLimit + 2)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > policyMachineWorkspaceLimit+1 {
		return policyMachineRefusal()
	}
	for _, entry := range names {
		if entry.Name() == policyExecutionDemandDirectory {
			if r.Version != 3 || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return policyMachineRefusal()
			}
			continue
		}
		known := false
		for _, w := range r.Workspaces {
			if entry.Name() == w.WorkspaceID && w.Executor != nil && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				known = true
				break
			}
		}
		if !known {
			return policyMachineRefusal()
		}
	}
	return m.executionRegistryCustody(r)
}

func (m *policyAuthorityMachine) executorOwnership(w policyMachineWorkspace, runnerWorkspace string) (*runnerExecutorOwnership, error) {
	if w.Executor == nil {
		return nil, nil
	}
	e := w.Executor
	if e.State != "enrolled" || m.custody() != nil {
		return nil, policyMachineRefusal()
	}
	root, _, err := openOwnFolder(m.root, []string{"executors", w.WorkspaceID}, false, "executor lease")
	if err != nil {
		return nil, policyMachineRefusal()
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || identityKey(info) != e.RootIdentity {
		return nil, policyMachineRefusal()
	}
	file, err := root.OpenFile(w.WorkspaceID+".lease", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, policyMachineRefusal()
	}
	defer file.Close()
	finfo, err := file.Stat()
	if err != nil {
		return nil, policyMachineRefusal()
	}
	links, linksKnown := linkCount(finfo)
	if ownerOnlyFile("executor lease", finfo.Mode()) != nil || ownedByUs("executor lease", finfo) != nil || !linksKnown || links != 1 || finfo.Size() != 0 || identityKey(finfo) != e.FileIdentity {
		return nil, policyMachineRefusal()
	}
	entry, err := root.Lstat(w.WorkspaceID + ".lease")
	if err != nil || !os.SameFile(finfo, entry) || m.custody() != nil {
		return nil, policyMachineRefusal()
	}
	rdev, rino, rok := executorIdentityParts(e.RootIdentity)
	fdev, fino, fok := executorIdentityParts(e.FileIdentity)
	if !rok || !fok {
		return nil, policyMachineRefusal()
	}
	return &runnerExecutorOwnership{Version: 1, Root: filepath.Join(m.options.Root, "executors", w.WorkspaceID), MachineWorkspace: w.WorkspaceID, RunnerWorkspace: runnerWorkspace, RootDevice: rdev, RootInode: rino, FileDevice: fdev, FileInode: fino}, nil
}

func (m *policyAuthorityMachine) executorForProject(project *ProjectRoot, runnerWorkspace string) (*runnerExecutorOwnership, error) {
	r, err := m.registry()
	if err != nil {
		return nil, err
	}
	w, found, err := policyMachineWorkspaceFor(r, project)
	if err != nil || !found {
		return nil, policyMachineRefusal()
	}
	return m.executorOwnership(w, runnerWorkspace)
}

// Called with the exclusive machine-use lease. Preserve the established order:
// ledger, registry, anchor. The anchor stays current through intent and custody
// publication. This creates no authorization to start or replay governed work.
func (m *policyAuthorityMachine) enrollExecutor(ctx context.Context, project *ProjectRoot, w policyMachineWorkspace) (PolicyAuthorityStatus, error) {
	bad := func() (PolicyAuthorityStatus, error) { return PolicyAuthorityStatus{}, policyMachineRefusal() }
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
	if err != nil {
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
	marker := &policyMachineMarker{Version: 1, InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID}
	if err != nil || !found || current.WorkspaceID != w.WorkspaceID || current.State != "enrolled" || current.Executor != nil || r.Generation > policyAuthorityAnchorMaxGeneration-2 || policyMachineMarkerMatches(project.own.root, marker) != nil {
		return bad()
	}
	a, err := newPolicyAuthorityAnchor(filepath.Join(m.options.Root, "anchors", w.WorkspaceID), policyAuthorityAnchorIdentity{InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		return bad()
	}
	a.checkCustody = m.custody
	a.openRoot = func(create bool) (*os.Root, error) {
		if create || m.custody() != nil {
			return nil, policyMachineRefusal()
		}
		root, _, e := openOwnFolder(m.root, []string{"anchors", w.WorkspaceID}, false, "Policy anchor")
		return root, e
	}
	var generation uint64
	err = a.WithCurrent(ctx, raw, func(held *policyAuthorityAnchorHeld) error {
		observation, e := held.Current()
		if e != nil {
			return policyMachineRefusal()
		}
		generation = observation.Generation
		nonce, e := NewToken()
		if e != nil || ctx.Err() != nil {
			return policyMachineRefusal()
		}
		index := -1
		for i := range r.Workspaces {
			if r.Workspaces[i].WorkspaceID == w.WorkspaceID {
				index = i
			}
		}
		if index < 0 {
			return policyMachineRefusal()
		}
		if r.Version < 2 {
			r.Version = 2
		}
		r.Workspaces[index].Executor = &policyMachineExecutor{State: "enrolling", Intent: &policyMachineIntent{Nonce: nonce, LedgerDigest: digestOf(raw)}}
		r.Generation++
		if m.persist("executor-intent", policyMachineRegistryName, m.root, r) != nil {
			return policyMachineRefusal()
		}
		completed := false
		defer func() {
			if !completed {
				m.poisoned.Store(true)
			}
		}()
		root, e := m.createExecutorDirectory(w.WorkspaceID)
		if e != nil {
			return policyMachineRefusal()
		}
		defer root.Close()
		if ctx.Err() != nil || m.persist("executor-file", w.WorkspaceID+".lease", root, []byte{}) != nil {
			return policyMachineRefusal()
		}
		rootInfo, e := root.Stat(".")
		if e != nil {
			return policyMachineRefusal()
		}
		fileInfo, e := root.Lstat(w.WorkspaceID + ".lease")
		if e != nil {
			return policyMachineRefusal()
		}
		e = nil
		r.Workspaces[index].Executor = &policyMachineExecutor{State: "enrolled", RootIdentity: identityKey(rootInfo), FileIdentity: identityKey(fileInfo)}
		if _, e = m.executorOwnership(r.Workspaces[index], "enrollment"); e != nil {
			return policyMachineRefusal()
		}
		r.Generation++
		if ctx.Err() != nil || m.custody() != nil || m.persist("executor-final", policyMachineRegistryName, m.root, r) != nil || ctx.Err() != nil || m.custody() != nil {
			return policyMachineRefusal()
		}
		completed = true
		return nil
	})
	if err != nil {
		return bad()
	}
	return PolicyAuthorityStatus{Version: 1, InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID, State: "current", LedgerDigest: digestOf(raw), AnchorGeneration: generation, ExecutorState: "enrolled"}, nil
}

func (m *policyAuthorityMachine) createExecutorDirectory(workspace string) (*os.Root, error) {
	if _, err := m.root.Lstat("executors"); errors.Is(err, os.ErrNotExist) {
		if m.root.Mkdir("executors", 0700) != nil || m.syncExecutorDirectory("executors-directory", m.root) != nil {
			return nil, policyMachineRefusal()
		}
	} else if err != nil {
		return nil, policyMachineRefusal()
	}
	parent, _, err := openOwnFolder(m.root, []string{"executors"}, false, "executor leases")
	if err != nil {
		return nil, policyMachineRefusal()
	}
	defer parent.Close()
	// Never reuse even an empty workspace directory from a partial attempt.
	if parent.Mkdir(workspace, 0700) != nil || m.syncExecutorDirectory("executor-directory", parent) != nil {
		return nil, policyMachineRefusal()
	}
	root, _, err := openOwnFolder(parent, []string{workspace}, false, "executor lease")
	return root, err
}

func (m *policyAuthorityMachine) syncExecutorDirectory(phase string, root *os.Root) error {
	if m.persistForTest != nil {
		return m.persistForTest(phase, root, "", nil)
	}
	return syncPrivateDirectory(root)
}
