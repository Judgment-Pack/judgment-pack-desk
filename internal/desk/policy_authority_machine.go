package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// PolicyAuthorityOptions is trusted host configuration, never portable settings
// or a request field. Both values must remain in the configured launcher.
type PolicyAuthorityOptions struct {
	Root           string
	InstallationID string
}

type PolicyAuthorityStatus struct {
	Version          int    `json:"version"`
	InstallationID   string `json:"installationId"`
	WorkspaceID      string `json:"workspaceId,omitempty"`
	State            string `json:"state"`
	LedgerDigest     string `json:"ledgerDigest,omitempty"`
	AnchorGeneration uint64 `json:"anchorGeneration,omitempty"`
	ExecutorState    string `json:"executorState,omitempty"`
}

const policyMachineRegistryName = "registry.json"
const policyMachineLeaseName = ".machine-use.lock"
const policyMachineMarkerName = "policy-authority.json"
const policyMachineRegistryLimit = 1 << 20
const policyMachineWorkspaceLimit = 256

type policyMachineIntent struct {
	Nonce        string `json:"nonce"`
	LedgerDigest string `json:"ledgerDigest"`
}
type policyMachineWorkspace struct {
	WorkspaceID     string                 `json:"workspaceId"`
	ProjectPath     string                 `json:"projectPath"`
	ProjectIdentity string                 `json:"projectIdentity"`
	State           string                 `json:"state"`
	Intent          *policyMachineIntent   `json:"intent,omitempty"`
	Executor        *policyMachineExecutor `json:"executor,omitempty"`
}
type policyMachineRegistry struct {
	Version        int                      `json:"version"`
	InstallationID string                   `json:"installationId"`
	RootIdentity   string                   `json:"rootIdentity"`
	Generation     uint64                   `json:"generation"`
	Workspaces     []policyMachineWorkspace `json:"workspaces"`
}
type policyMachineMarker struct {
	Version        int    `json:"version"`
	InstallationID string `json:"installationId"`
	WorkspaceID    string `json:"workspaceId"`
}
type policyAuthorityMachine struct {
	options   PolicyAuthorityOptions
	root      *os.Root
	lease     *os.File
	info      os.FileInfo
	leaseInfo os.FileInfo
	poisoned  atomic.Bool
	mu        sync.Mutex
	closing   bool
	work      sync.WaitGroup
	closeOnce sync.Once
	// Object-local fault injection; production always uses writePrivateData.
	persistForTest func(string, *os.Root, string, []byte) error
}

func policyMachineRefusal() error {
	f := policyRefusal(503, "authority-unavailable", "Machine Policy authority is unavailable or unresolved. Preserve its trusted configuration and use read-only recovery; do not initialize or replay it.")
	f.Uncertain = true
	return f
}
func policyMachineSeparate(machine string, paths ...string) error {
	for _, path := range paths {
		if path != "" && (pathContains(machine, path) || pathContains(path, machine)) {
			return policyMachineRefusal()
		}
	}
	return nil
}
func policyMachineOptionsValid(o PolicyAuthorityOptions) bool {
	return filepath.IsAbs(o.Root) && filepath.Clean(o.Root) == o.Root && o.Root != string(filepath.Separator) && len(o.Root) <= 2048 && !strings.ContainsAny(o.Root, "\x00\r\n") && retainedArtifactDigest.MatchString(o.InstallationID)
}
func readPolicyMachineMarker(project *os.Root) (*policyMachineMarker, error) {
	return inspectPolicyMachineMarker(project, nil)
}

// Absent demands do not turn a permissive legacy audit folder into a fatal
// startup error. Pin that ordinary folder without changing it; only a present
// marker requires private custody. An ambiguous or replaced parent is no absence.
func inspectPolicyMachineMarker(project *os.Root, afterPin func()) (*policyMachineMarker, error) {
	info, err := project.Lstat(startupIdentityDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, policyMachineRefusal()
	}
	private, err := project.OpenRoot(startupIdentityDir)
	if err != nil {
		return nil, policyMachineRefusal()
	}
	defer private.Close()
	unchanged := func(requirePrivate bool) bool {
		entry, e := project.Lstat(startupIdentityDir)
		if e != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entry) {
			return false
		}
		held, e := private.Stat(".")
		if e != nil || !os.SameFile(info, held) {
			return false
		}
		return !requirePrivate || entry.Mode().Perm()&0077 == 0 && held.Mode().Perm()&0077 == 0 && ownedByUs("Policy authority demand", entry) == nil && ownedByUs("Policy authority demand", held) == nil
	}
	if !unchanged(false) {
		return nil, policyMachineRefusal()
	}
	if afterPin != nil {
		afterPin()
	}
	_, err = private.Lstat(policyMachineMarkerName)
	if errors.Is(err, os.ErrNotExist) && unchanged(false) {
		return nil, nil
	}
	if err != nil || !unchanged(true) {
		return nil, policyMachineRefusal()
	}
	raw, err := readPrivateData(private, policyMachineMarkerName, 1024)
	var marker policyMachineMarker
	if err != nil || !unchanged(true) || rejectDuplicateMembers(raw) != nil || policyAuthorityAnchorObject(raw, []string{"version", "installationId", "workspaceId"}, "") != nil || decodeDataJSON(raw, &marker) != nil || marker.Version != 1 || !retainedArtifactDigest.MatchString(marker.InstallationID) || !retainedArtifactDigest.MatchString(marker.WorkspaceID) {
		return nil, policyMachineRefusal()
	}
	return &marker, nil
}
func policyMachineNoDemand(project *os.Root) error {
	marker, err := readPolicyMachineMarker(project)
	if err != nil || marker != nil {
		return policyMachineRefusal()
	}
	return nil
}
func decodePolicyMachineRegistry(raw []byte) (policyMachineRegistry, error) {
	var r policyMachineRegistry
	if len(raw) > policyMachineRegistryLimit || rejectDuplicateMembers(raw) != nil || policyAuthorityAnchorObject(raw, []string{"version", "installationId", "rootIdentity", "generation", "workspaces"}, "") != nil || decodeDataJSON(raw, &r) != nil || (r.Version != 1 && r.Version != 2) || !retainedArtifactDigest.MatchString(r.InstallationID) || !fileIdentityForm.MatchString(r.RootIdentity) || r.Generation == 0 || r.Generation > policyAuthorityAnchorMaxGeneration || r.Workspaces == nil || len(r.Workspaces) > policyMachineWorkspaceLimit {
		return r, policyMachineRefusal()
	}
	var wire struct {
		Workspaces []json.RawMessage `json:"workspaces"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return r, policyMachineRefusal()
	}
	ids, paths, folders := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, w := range r.Workspaces {
		if policyMachineWorkspaceShape(wire.Workspaces[i], r.Version) != nil || !retainedArtifactDigest.MatchString(w.WorkspaceID) || !validProjectHistoryPath(w.ProjectPath) || len(w.ProjectPath) > 2048 || filepath.Clean(w.ProjectPath) != w.ProjectPath || !fileIdentityForm.MatchString(w.ProjectIdentity) || ids[w.WorkspaceID] || paths[w.ProjectPath] || folders[w.ProjectIdentity] || !contains([]string{"legacy", "enrolling", "enrolled"}, w.State) || (w.State == "enrolling") != (w.Intent != nil) {
			return r, policyMachineRefusal()
		}
		if policyMachineExecutorShape(wire.Workspaces[i], w) != nil {
			return r, policyMachineRefusal()
		}
		if w.Intent != nil {
			var v struct {
				Intent json.RawMessage `json:"intent"`
			}
			_ = json.Unmarshal(wire.Workspaces[i], &v)
			if policyAuthorityAnchorObject(v.Intent, []string{"nonce", "ledgerDigest"}, "") != nil || !policyAuthorityAnchorNonceForm.MatchString(w.Intent.Nonce) || !retainedArtifactDigest.MatchString(w.Intent.LedgerDigest) {
				return r, policyMachineRefusal()
			}
		}
		ids[w.WorkspaceID], paths[w.ProjectPath], folders[w.ProjectIdentity] = true, true, true
	}
	return r, nil
}
func (m *policyAuthorityMachine) close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.stopAdmission()
		m.work.Wait()
		if m.lease != nil {
			_ = m.lease.Close()
		}
		if m.root != nil {
			_ = m.root.Close()
		}
	})
}
func (m *policyAuthorityMachine) stopAdmission() {
	if m != nil {
		m.mu.Lock()
		m.closing = true
		m.mu.Unlock()
	}
}
func (m *policyAuthorityMachine) admit() (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return nil, policyMachineRefusal()
	}
	m.work.Add(1)
	return m.work.Done, nil
}
func openPolicyMachine(o PolicyAuthorityOptions, exclusive bool) (*policyAuthorityMachine, error) {
	if !policyMachineOptionsValid(o) {
		return nil, policyMachineRefusal()
	}
	root, err := openPrivateDataRoot(o.Root, false)
	if err != nil {
		return nil, policyMachineRefusal()
	}
	m := &policyAuthorityMachine{options: o, root: root}
	fail := func() (*policyAuthorityMachine, error) { m.close(); return nil, policyMachineRefusal() }
	m.info, err = root.Stat(".")
	if err != nil {
		return fail()
	}
	before, err := root.Lstat(policyMachineLeaseName)
	if err != nil || ownerOnlyFile("machine lease", before.Mode()) != nil || ownedByUs("machine lease", before) != nil {
		return fail()
	}
	m.lease, err = root.OpenFile(policyMachineLeaseName, os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return fail()
	}
	m.leaseInfo, err = m.lease.Stat()
	if err != nil || !os.SameFile(before, m.leaseInfo) || lockPrivateData(m.lease, exclusive) != nil {
		return fail()
	}
	if _, err = m.registry(); err != nil {
		return fail()
	}
	return m, nil
}
func (m *policyAuthorityMachine) custody() error {
	if m == nil || m.poisoned.Load() {
		return policyMachineRefusal()
	}
	now, err := openPrivateDataRoot(m.options.Root, false)
	if err != nil {
		return policyMachineRefusal()
	}
	defer now.Close()
	info, err := now.Stat(".")
	if err != nil || !os.SameFile(m.info, info) {
		return policyMachineRefusal()
	}
	lease, err := now.Lstat(policyMachineLeaseName)
	if err != nil || !os.SameFile(m.leaseInfo, lease) {
		return policyMachineRefusal()
	}
	return nil
}

// Caller holds a machine-use lease. Registry mutation additionally requires EX.
func (m *policyAuthorityMachine) registry() (policyMachineRegistry, error) {
	lock, err := m.root.Open(".")
	if err != nil {
		return policyMachineRegistry{}, policyMachineRefusal()
	}
	defer lock.Close()
	if lockPrivateData(lock, false) != nil {
		return policyMachineRegistry{}, policyMachineRefusal()
	}
	return m.readRegistry()
}
func (m *policyAuthorityMachine) readRegistry() (policyMachineRegistry, error) {
	var r policyMachineRegistry
	if m.custody() != nil {
		return r, policyMachineRefusal()
	}
	raw, err := readPrivateData(m.root, policyMachineRegistryName, policyMachineRegistryLimit)
	if err != nil {
		return r, policyMachineRefusal()
	}
	r, err = decodePolicyMachineRegistry(raw)
	if err != nil || r.InstallationID != m.options.InstallationID || r.RootIdentity != identityKey(m.info) {
		return r, policyMachineRefusal()
	}
	if m.executorRegistryCustody(r) != nil {
		return r, policyMachineRefusal()
	}
	// A rolled-back legacy classification cannot hide an existing/orphan anchor.
	anchors, _, err := openOwnFolder(m.root, []string{"anchors"}, false, "Policy anchors")
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, policyMachineRefusal()
	}
	defer anchors.Close()
	entries, err := anchors.Open(".")
	if err != nil {
		return r, policyMachineRefusal()
	}
	defer entries.Close()
	names, err := entries.ReadDir(policyMachineWorkspaceLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > policyMachineWorkspaceLimit {
		return r, policyMachineRefusal()
	}
	for _, entry := range names {
		known := false
		for _, w := range r.Workspaces {
			if entry.Name() == w.WorkspaceID && w.State != "legacy" && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				known = true
				break
			}
		}
		if !known {
			return r, policyMachineRefusal()
		}
	}
	return r, nil
}
func (m *policyAuthorityMachine) persist(phase, name string, root *os.Root, value any) error {
	raw, err := json.Marshal(value)
	if phase == "executor-file" {
		raw = nil
	}
	if err != nil || len(raw) > policyMachineRegistryLimit {
		return policyMachineRefusal()
	}
	if m.persistForTest != nil {
		err = m.persistForTest(phase, root, name, raw)
	} else if phase == "marker" || phase == "executor-file" {
		err = claimPrivateData(root, name, raw)
		if err == nil {
			var retained []byte
			retained, err = readPrivateData(root, name, len(raw))
			if err == nil && string(retained) != string(raw) {
				err = policyMachineRefusal()
			}
		}
	} else {
		err = writePrivateData(root, name, raw)
	}
	if err != nil {
		m.poisoned.Store(true)
		return policyMachineRefusal()
	}
	return nil
}
func policyMachineWorkspaceFor(r policyMachineRegistry, p *ProjectRoot) (policyMachineWorkspace, bool, error) {
	if !utf8.ValidString(p.dir) {
		return policyMachineWorkspace{}, false, policyMachineRefusal()
	}
	info, err := p.own.root.Stat(".")
	if err != nil || identityKey(info) == "" || !os.SameFile(p.info, info) || !sameDirectory(p.dir, info) {
		return policyMachineWorkspace{}, false, policyMachineRefusal()
	}
	for _, w := range r.Workspaces {
		if w.ProjectPath == p.dir || w.ProjectIdentity == identityKey(info) {
			if w.ProjectPath != p.dir || w.ProjectIdentity != identityKey(info) {
				return w, false, policyMachineRefusal()
			}
			return w, true, nil
		}
	}
	return policyMachineWorkspace{}, false, nil
}
func (m *policyAuthorityMachine) resolve(p *ProjectRoot) (*policyAuthorityAnchor, error) {
	if policyMachineSeparate(m.options.Root, p.dir) != nil {
		return nil, policyMachineRefusal()
	}
	r, err := m.registry()
	if err != nil {
		return nil, err
	}
	w, found, err := policyMachineWorkspaceFor(r, p)
	if err != nil || !found || w.State == "enrolling" || w.Executor != nil && w.Executor.State != "enrolled" {
		return nil, policyMachineRefusal()
	}
	marker, err := readPolicyMachineMarker(p.own.root)
	if err != nil {
		return nil, err
	}
	if w.State == "legacy" {
		if marker != nil {
			return nil, policyMachineRefusal()
		}
		return nil, nil
	}
	if marker == nil || marker.InstallationID != r.InstallationID || marker.WorkspaceID != w.WorkspaceID {
		return nil, policyMachineRefusal()
	}
	a, err := newPolicyAuthorityAnchor(filepath.Join(m.options.Root, "anchors", w.WorkspaceID), policyAuthorityAnchorIdentity{InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		return nil, policyMachineRefusal()
	}
	a.checkCustody = m.custody
	a.openRoot = func(create bool) (*os.Root, error) {
		if create || m.custody() != nil {
			return nil, policyMachineRefusal()
		}
		current, e := m.registry()
		if e != nil {
			return nil, e
		}
		exact, ok, e := policyMachineWorkspaceFor(current, p)
		if e != nil || !ok || exact.State != "enrolled" || exact.WorkspaceID != w.WorkspaceID || policyMachineMarkerMatches(p.own.root, marker) != nil {
			return nil, policyMachineRefusal()
		}
		root, _, e := openOwnFolder(m.root, []string{"anchors", w.WorkspaceID}, false, "Policy anchor")
		if e == nil && m.custody() != nil {
			root.Close()
			return nil, policyMachineRefusal()
		}
		return root, e
	}
	return a, nil
}

// PolicyAuthorityLocal is the separate trusted local operator boundary. It
// starts no Server, Runtime, listener or worker and never retries enrollment.
func PolicyAuthorityLocal(ctx context.Context, action string, o PolicyAuthorityOptions, projectDir string) (PolicyAuthorityStatus, error) {
	return policyAuthorityLocal(ctx, action, o, projectDir, nil)
}
func policyAuthorityLocal(ctx context.Context, action string, o PolicyAuthorityOptions, projectDir string, hook func(string, *os.Root, string, []byte) error) (PolicyAuthorityStatus, error) {
	var status PolicyAuthorityStatus
	if ctx.Err() != nil || !contains([]string{"init", "status", "classify-legacy", "enroll", "enroll-executor"}, action) {
		return status, policyMachineRefusal()
	}
	config, err := ResolveDeskConfigDir()
	if err != nil {
		return status, policyMachineRefusal()
	}
	if action != "init" && !policyMachineOptionsValid(o) {
		return status, policyMachineRefusal()
	}
	if err = policyMachineStartupJournals(); err != nil {
		return status, err
	}
	var project *ProjectRoot
	if projectDir != "" {
		if !filepath.IsAbs(projectDir) {
			return status, policyMachineRefusal()
		}
		project, err = OpenProjectRoot(projectDir)
		if err != nil {
			return status, policyMachineRefusal()
		}
		defer project.Close()
	}
	if (action == "enroll" || action == "enroll-executor" || action == "classify-legacy") && project == nil {
		return status, policyMachineRefusal()
	}
	if err = policyMachineProtectedPaths(o.Root, config, project); err != nil {
		return status, err
	}
	if action == "init" {
		return initializePolicyMachine(ctx, o, hook)
	}
	m, err := openPolicyMachine(o, action != "status")
	if err != nil {
		return status, err
	}
	defer m.close()
	m.persistForTest = hook
	r, err := m.registry()
	if err != nil {
		return status, err
	}
	status = PolicyAuthorityStatus{Version: 1, InstallationID: r.InstallationID, State: "ready"}
	if project == nil {
		return status, nil
	}
	w, found, err := policyMachineWorkspaceFor(r, project)
	if err != nil {
		return PolicyAuthorityStatus{}, err
	}
	if action == "status" {
		if !found {
			status.State = "unclassified"
			return status, nil
		}
		status.WorkspaceID, status.State = w.WorkspaceID, w.State
		status.ExecutorState = "unenrolled"
		if w.Executor != nil {
			status.ExecutorState = w.Executor.State
			if w.Executor.State != "enrolled" {
				return status, nil
			}
			if _, e := m.executorOwnership(w, "status"); e != nil {
				status.ExecutorState = "unavailable"
			}
		}
		if w.State != "enrolled" {
			return status, nil
		}
		policies, _, e := openOwnFolder(project.own.root, strings.Split(policyDirectory, "/"), false, "policies")
		if e != nil {
			status.State = "ledger-unavailable"
			return status, nil
		}
		defer policies.Close()
		lock, e := policies.Open(".")
		if e != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		defer lock.Close()
		if waitPrivateDataLock(ctx, lock, false) != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		raw, _, e := readExistingPolicyLedger(policies)
		if e != nil {
			status.State = "ledger-unavailable"
			return status, nil
		}
		status.LedgerDigest = digestOf(raw)
		a, e := m.resolve(project)
		if e != nil {
			return PolicyAuthorityStatus{}, e
		}
		root, alock, e := a.open(ctx, false)
		if e != nil {
			status.State = "anchor-unavailable"
			return status, nil
		}
		defer root.Close()
		defer alock.Close()
		state, e := a.read(root)
		if e != nil {
			status.State = "anchor-unavailable"
			return status, nil
		}
		observation, e := classifyPolicyAuthorityAnchor(state, a.identity, status.LedgerDigest)
		if e != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		status.State, status.AnchorGeneration = observation.Classification, observation.Generation
		return status, nil
	}
	if err = policyMachineProjectIdle(config, project); err != nil {
		return PolicyAuthorityStatus{}, err
	}
	if action == "enroll-executor" {
		if !found || w.State != "enrolled" || w.Executor != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		return m.enrollExecutor(ctx, project, w)
	}
	if action == "classify-legacy" {
		if found || policyMachineNoDemand(project.own.root) != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		lock, e := m.root.Open(".")
		if e != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		defer lock.Close()
		if lockPrivateData(lock, true) != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		if len(r.Workspaces) >= policyMachineWorkspaceLimit || r.Generation == policyAuthorityAnchorMaxGeneration {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		w = policyMachineWorkspace{WorkspaceID: randomStartupID(), ProjectPath: project.dir, ProjectIdentity: identityKey(project.info), State: "legacy"}
		r.Workspaces = append(r.Workspaces, w)
		r.Generation++
		if err = m.persist("legacy", policyMachineRegistryName, m.root, r); err != nil {
			return PolicyAuthorityStatus{}, err
		}
		if m.custody() != nil || ctx.Err() != nil {
			return PolicyAuthorityStatus{}, policyMachineRefusal()
		}
		status.WorkspaceID, status.State = w.WorkspaceID, w.State
		return status, nil
	}
	// Enrollment is allowed only from an explicit known-legacy classification.
	if !found || w.State != "legacy" || policyMachineNoDemand(project.own.root) != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	policies, _, err := openOwnFolder(project.own.root, strings.Split(policyDirectory, "/"), false, "policies")
	if err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	defer policies.Close()
	ledgerLock, err := policies.Open(".")
	if err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	defer ledgerLock.Close()
	if waitPrivateDataLock(ctx, ledgerLock, true) != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	raw, ledger, err := readExistingPolicyLedger(policies)
	if err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	if _, err = policyArchiveClosure(ctx, ledger, retainedArtifactsForProject(project.own.root)); err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	registryLock, err := m.root.Open(".")
	if err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	defer registryLock.Close()
	if lockPrivateData(registryLock, true) != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	r, err = m.readRegistry()
	if err != nil {
		return PolicyAuthorityStatus{}, err
	}
	current, ok, err := policyMachineWorkspaceFor(r, project)
	if err != nil || !ok || current != w || r.Generation > policyAuthorityAnchorMaxGeneration-2 || ctx.Err() != nil || policyMachineNoDemand(project.own.root) != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	nonce, e := NewToken()
	if e != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	for i := range r.Workspaces {
		if r.Workspaces[i].WorkspaceID == w.WorkspaceID {
			r.Workspaces[i].State = "enrolling"
			r.Workspaces[i].Intent = &policyMachineIntent{Nonce: nonce, LedgerDigest: digestOf(raw)}
		}
	}
	r.Generation++
	if err = m.persist("intent", policyMachineRegistryName, m.root, r); err != nil {
		return PolicyAuthorityStatus{}, err
	}
	// Confirmed intent holds until finalization. A final-write error may leave
	// stable enrolled storage, but this caller still receives no success/replay.
	completed := false
	defer func() {
		if !completed {
			m.poisoned.Store(true)
		}
	}()
	private, _, err := openOwnFolder(project.own.root, []string{startupIdentityDir}, false, "Policy authority demand")
	if err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	defer private.Close()
	marker := policyMachineMarker{Version: 1, InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID}
	if ctx.Err() != nil || m.persist("marker", policyMachineMarkerName, private, marker) != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	a, err := newPolicyAuthorityAnchor(filepath.Join(o.Root, "anchors", w.WorkspaceID), policyAuthorityAnchorIdentity{InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	a.openRoot = func(create bool) (*os.Root, error) {
		if m.custody() != nil {
			return nil, policyMachineRefusal()
		}
		return m.anchorDirectory(w.WorkspaceID, create)
	}
	if hook != nil {
		a.persistForTest = func(_ policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
			return hook("anchor", root, policyAuthorityAnchorName, raw)
		}
	}
	if err = a.Enroll(ctx, raw); err != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	for i := range r.Workspaces {
		if r.Workspaces[i].WorkspaceID == w.WorkspaceID {
			r.Workspaces[i].State = "enrolled"
			r.Workspaces[i].Intent = nil
		}
	}
	r.Generation++
	if ctx.Err() != nil || m.custody() != nil || m.persist("final", policyMachineRegistryName, m.root, r) != nil || ctx.Err() != nil || m.custody() != nil {
		return PolicyAuthorityStatus{}, policyMachineRefusal()
	}
	completed = true
	return PolicyAuthorityStatus{Version: 1, InstallationID: r.InstallationID, WorkspaceID: w.WorkspaceID, State: "enrolled", LedgerDigest: digestOf(raw), AnchorGeneration: 1}, nil
}

// Durably publish each new directory entry through its already-pinned parent.
// openOwnFolder alone does not sync newly created relative parents.
func (m *policyAuthorityMachine) anchorDirectory(workspace string, create bool) (*os.Root, error) {
	current := m.root
	var owned *os.Root
	for i, part := range []string{"anchors", workspace} {
		if create {
			_, err := current.Lstat(part)
			if errors.Is(err, os.ErrNotExist) {
				err = current.Mkdir(part, 0700)
				if err == nil {
					phase := "anchors-directory"
					if i == 1 {
						phase = "workspace-directory"
					}
					if m.persistForTest != nil {
						err = m.persistForTest(phase, current, "", nil)
					} else {
						err = syncPrivateDirectory(current)
					}
				}
			}
			if err != nil {
				if owned != nil {
					owned.Close()
				}
				return nil, policyMachineRefusal()
			}
		}
		next, _, err := openOwnFolder(current, []string{part}, false, "Policy anchor")
		if owned != nil {
			owned.Close()
		}
		if err != nil {
			return nil, policyMachineRefusal()
		}
		owned = next
		current = next
	}
	return owned, nil
}
func initializePolicyMachine(ctx context.Context, o PolicyAuthorityOptions, hook func(string, *os.Root, string, []byte) error) (PolicyAuthorityStatus, error) {
	var status PolicyAuthorityStatus
	if o.InstallationID != "" || !filepath.IsAbs(o.Root) || filepath.Clean(o.Root) != o.Root || o.Root == string(filepath.Separator) || len(o.Root) > 2048 || strings.ContainsAny(o.Root, "\x00\r\n") {
		return status, policyMachineRefusal()
	}
	parent, err := OpenProjectRoot(filepath.Dir(o.Root))
	if err != nil {
		return status, policyMachineRefusal()
	}
	defer parent.Close()
	if parent.dir != filepath.Dir(o.Root) || safeMoveParent(parent.info) != nil || ctx.Err() != nil {
		return status, policyMachineRefusal()
	}
	// Atomic non-replacing mkdir: even an empty/partial prior leaf is never reset.
	if parent.own.root.Mkdir(filepath.Base(o.Root), 0700) != nil || syncPrivateDirectory(parent.own.root) != nil {
		return status, policyMachineRefusal()
	}
	root, err := openPrivateDataRoot(o.Root, false)
	if err != nil {
		return status, policyMachineRefusal()
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || identityKey(info) == "" {
		return status, policyMachineRefusal()
	}
	if claimPrivateData(root, policyMachineLeaseName, []byte("policy-authority-machine/1\n")) != nil {
		return status, policyMachineRefusal()
	}
	lease, err := root.OpenFile(policyMachineLeaseName, os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return status, policyMachineRefusal()
	}
	defer lease.Close()
	if lockPrivateData(lease, true) != nil {
		return status, policyMachineRefusal()
	}
	o.InstallationID = randomStartupID()
	m := &policyAuthorityMachine{options: o, root: root, info: info, persistForTest: hook}
	m.leaseInfo, err = lease.Stat()
	if err != nil {
		return status, policyMachineRefusal()
	}
	registry := policyMachineRegistry{Version: 1, InstallationID: o.InstallationID, RootIdentity: identityKey(info), Generation: 1, Workspaces: []policyMachineWorkspace{}}
	if ctx.Err() != nil || m.custody() != nil || m.persist("init", policyMachineRegistryName, root, registry) != nil || ctx.Err() != nil || m.custody() != nil {
		return status, policyMachineRefusal()
	}
	return PolicyAuthorityStatus{Version: 1, InstallationID: o.InstallationID, State: "ready"}, nil
}
func policyMachineMarkerMatches(root *os.Root, want *policyMachineMarker) error {
	got, err := readPolicyMachineMarker(root)
	if err != nil || got == nil || want == nil || *got != *want {
		return policyMachineRefusal()
	}
	return nil
}
