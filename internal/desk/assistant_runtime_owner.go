package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"syscall"
)

// assistantRuntimeArtifactSelection classifies only an executable selected by
// trusted host startup configuration. Its local route verifies exact custody;
// it makes no assertion about who published those bytes.
type assistantRuntimeArtifactSelection struct {
	path           string
	expectedDigest string
	bundled        bool
}

func selectAssistantRuntimeArtifact(source string, held *os.File, deskExecutable, releaseDigest string) (assistantRuntimeArtifactSelection, error) {
	selected, err := filepath.Abs(source)
	if err != nil {
		return assistantRuntimeArtifactSelection{}, err
	}
	result := assistantRuntimeArtifactSelection{path: selected}
	if releaseDigest == "" {
		return result, nil
	}
	// Resolve the deployed Desk's symlink before locating its sibling companion.
	// An ambiguous deployment location cannot silently become local trust.
	executable, err := filepath.EvalSymlinks(deskExecutable)
	if err != nil {
		return result, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return result, err
	}
	bundled := filepath.Join(filepath.Dir(executable), "jpack")
	result.bundled = filepath.Clean(selected) == bundled
	resolved, err := filepath.EvalSymlinks(selected)
	if err == nil && filepath.Clean(resolved) == bundled {
		result.bundled = true
	}
	if err != nil && !result.bundled {
		return result, err
	}
	sourceInfo, err := held.Stat()
	if err != nil {
		return result, err
	}
	// A hard link or another symlink to the actual companion is also bundled.
	// Path equality above remains binding even if its inode has been replaced.
	if bundledInfo, statErr := os.Stat(bundled); statErr == nil && os.SameFile(sourceInfo, bundledInfo) {
		result.bundled = true
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return result, statErr
	}
	if result.bundled {
		result.expectedDigest = releaseDigest
	}
	return result, nil
}

func openSelectedAssistantRuntimeArtifact(ctx context.Context, source, deskExecutable, releaseDigest string) (*assistantRuntimeArtifact, error) {
	held, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer held.Close()
	info, err := held.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("selected runtime artifact is not a regular file")
	}
	selection, err := selectAssistantRuntimeArtifact(source, held, deskExecutable, releaseDigest)
	if err != nil {
		return nil, err
	}
	expected := selection.expectedDigest
	if !selection.bundled {
		// Host startup selection is the authority here, not a path furnished by an
		// adapter, browser, checkpoint or model. Measure and copy this SAME held fd.
		hash := sha256.New()
		if _, err := io.Copy(hash, assistantRuntimeArtifactReader{ctx: ctx, reader: io.NewSectionReader(held, 0, info.Size())}); err != nil {
			return nil, err
		}
		expected = hex.EncodeToString(hash.Sum(nil))
	}
	return holdAssistantRuntimeArtifact(ctx, held, expected)
}

// assistantRuntimeOwner describes the exact retained connector. File paths and
// credential-bearing environment/configuration are private; only closed owner
// digests and explanatory labels cross the Gate boundary.
type assistantRuntimeOwner struct {
	runtime     *assistantRuntime
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *os.File
	conn        *conn
	artifact    *assistantRuntimeArtifact
	authority   *assistantRunPhysicalAuthority
	environment []string
	scope       string
	scopeErr    error
	identity    gate.RuntimeExecutionOwner
}

func newAssistantRuntimeOwner(ctx context.Context, r *assistantRuntime, environment []string) (*assistantRuntimeOwner, error) {
	authority, err := holdAssistantRunPhysicalAuthority(r.execution)
	if err != nil {
		return nil, err
	}
	launch, err := NewToken()
	if err != nil {
		return nil, err
	}
	e := r.execution
	identity, _, err := e.service.server.assistantRunIdentity(e.request.WithContext(ctx))
	if err != nil || !assistantRunIdentityMatches(identity, e.record.Binding) {
		return nil, errAssistantRunScope
	}
	principal, _ := json.Marshal(struct {
		Authenticated        string
		UID, EUID, GID, EGID int
	}{identity.Principal, os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid()})
	o := &assistantRuntimeOwner{runtime: r, cmd: r.cmd, stdin: r.stdin, stdout: r.stdout, conn: r.conn, artifact: r.artifact, authority: authority, environment: slices.Clone(environment)}
	o.scope, o.scopeErr = observeAssistantRuntimeScope(ctx, e.service.server, o.environment)
	o.identity = gate.RuntimeExecutionOwner{ArtifactDigest: "sha256:" + r.artifact.digest, ProcessID: launch, ProjectID: e.record.Binding.Project, ProjectLabel: "Held Desk project", SessionID: identity.Session, PrincipalDigest: "sha256:" + digestOf(principal), PrincipalLabel: "Authenticated Desk operator", ScopeDigest: "sha256:" + o.scope, ScopeLabel: "Observed Runtime configuration and resource roots"}
	return o, nil
}

func (o *assistantRuntimeOwner) Current(ctx context.Context) (gate.RuntimeExecutionOwner, error) {
	if o.scopeErr != nil {
		return gate.RuntimeExecutionOwner{}, o.scopeErr
	}
	r := o.runtime
	if ctx.Err() != nil || r.ctx.Err() != nil || r.cmd != o.cmd || r.conn != o.conn || r.stdout != o.stdout || r.stdin != o.stdin || r.artifact != o.artifact || r.cmd.Process == nil {
		return gate.RuntimeExecutionOwner{}, errAssistantRunScope
	}
	select {
	case <-r.waitDone:
		return gate.RuntimeExecutionOwner{}, errAssistantRunScope
	default:
	}
	if err := r.execution.Authorize(ctx); err != nil {
		return gate.RuntimeExecutionOwner{}, err
	}
	freshAuthority, err := holdAssistantRunPhysicalAuthority(r.execution)
	if err != nil || freshAuthority.writer != o.authority.writer || freshAuthority.lease != o.authority.lease || freshAuthority.id != o.authority.id || o.authority.store != nil && freshAuthority.store != o.authority.store {
		return gate.RuntimeExecutionOwner{}, errAssistantRunScope
	}
	if err := freshAuthority.write(ctx, func() error { return nil }); err != nil {
		return gate.RuntimeExecutionOwner{}, err
	}
	if err := o.artifact.Current(); err != nil {
		return gate.RuntimeExecutionOwner{}, err
	}
	fresh, err := observeAssistantRuntimeScope(ctx, r.execution.service.server, o.environment)
	if err != nil || fresh != o.scope {
		return gate.RuntimeExecutionOwner{}, errAssistantRunScope
	}
	return o.identity, nil
}

// An unavailable authoritative observation refuses acting use. Read and pure
// tools retain their existing path through the same verified transport.
func (o *assistantRuntimeOwner) port() *gate.RuntimeActionPort {
	if o == nil || o.scopeErr != nil || o.authority.store == nil {
		return nil
	}
	return &gate.RuntimeActionPort{Owner: o.identity, Current: o.Current}
}

// Scope identifies authority locations and selection, not mutable audit output
// contents. Known absence is observed explicitly; unreadable/ambiguous state
// refuses acting use. Configuration itself is reread as bounded regular bytes.
type assistantRuntimeScopeEntry struct {
	Path      string
	Identity  string
	Present   bool
	Directory bool
	Mode      os.FileMode
}

func observeAssistantRuntimeScope(ctx context.Context, s *Server, environment []string) (string, error) {
	if ctx.Err() != nil || s.project == nil || !sameDirectory(s.projectDir, s.project.info) || s.location.moving.Load() || s.projectHasMoved() {
		return "", errAssistantRunScope
	}
	held, err := s.project.own.dirFile.Stat()
	if err != nil || !os.SameFile(held, s.project.info) {
		return "", errAssistantRunScope
	}
	env := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	config := strings.TrimSpace(env[runtimeConfigEnv])
	if config == "" {
		config = "jpack.json"
	}
	if !filepath.IsAbs(config) {
		config = filepath.Join(s.projectDir, config)
	}
	config = filepath.Clean(config)
	base := filepath.Dir(config)
	baseInfo, err := os.Stat(base)
	if err != nil || !baseInfo.IsDir() {
		return "", errAssistantRunScope
	}
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", errAssistantRunScope
	}
	entries := []assistantRuntimeScopeEntry{}
	record := func(path string, directory bool) error {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			entries = append(entries, assistantRuntimeScopeEntry{Path: path})
			return nil
		}
		if err != nil || directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
			return errAssistantRunScope
		}
		identity := identityKey(info)
		if identity == "" {
			return errAssistantRunScope
		}
		entries = append(entries, assistantRuntimeScopeEntry{Path: path, Identity: identity, Present: true, Directory: info.IsDir(), Mode: info.Mode()})
		return nil
	}
	if err := record(s.projectDir, true); err != nil {
		return "", err
	}
	if err := record(base, true); err != nil {
		return "", err
	}
	raw, configInfo, err := readAssistantRuntimeScopeFile(ctx, config, 1<<20)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return "", err
	}
	configDigest := "absent"
	configIdentity := "absent"
	signingKey := ""
	if !missing {
		resolvedConfig, resolveErr := filepath.EvalSymlinks(config)
		relative, relativeErr := filepath.Rel(baseResolved, resolvedConfig)
		if resolveErr != nil || relativeErr != nil || !filepath.IsLocal(relative) {
			return "", errAssistantRunScope
		}
		if !uniqueJSON(raw) {
			return "", errAssistantRunScope
		}
		var doc map[string]json.RawMessage
		if json.Unmarshal(raw, &doc) != nil || doc == nil {
			return "", errAssistantRunScope
		}
		for key := range doc {
			if !slices.Contains([]string{"configVersion", "packs", "graphs", "audit", "requireReviewed", "requireComparableFacts"}, key) {
				return "", errAssistantRunScope
			}
		}
		var version string
		if json.Unmarshal(doc["configVersion"], &version) != nil || !slices.Contains([]string{"1", "2", "3", "4", "5", "6", "7"}, version) {
			return "", errAssistantRunScope
		}
		configDigest = digestOf(raw)
		configIdentity = identityKey(configInfo)
		if configIdentity == "" {
			return "", errAssistantRunScope
		}
		observeResource := func(name string, directory bool) error {
			if !filepath.IsLocal(name) {
				return errAssistantRunScope
			}
			path := filepath.Join(base, name)
			// Observe every parent identity: replacing an intermediate resource root
			// cannot retain authority merely because its pathname stayed the same.
			for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
				if err := record(filepath.Join(base, parent), true); err != nil {
					return err
				}
			}
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				relative, err := filepath.Rel(baseResolved, resolved)
				if err != nil || !filepath.IsLocal(relative) {
					return errAssistantRunScope
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return errAssistantRunScope
			}
			return record(path, directory)
		}
		for _, catalogue := range []struct {
			name  string
			paths []string
		}{{"packs", []string{"path", "matrix"}}, {"graphs", []string{"path", "rows"}}} {
			if doc[catalogue.name] == nil {
				if catalogue.name == "packs" {
					return "", errAssistantRunScope
				}
				continue
			}
			var resources map[string]map[string]json.RawMessage
			if json.Unmarshal(doc[catalogue.name], &resources) != nil || resources == nil {
				return "", errAssistantRunScope
			}
			keys := make([]string, 0, len(resources))
			for key := range resources {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				resource := resources[key]
				if resource == nil {
					return "", errAssistantRunScope
				}
				for _, member := range catalogue.paths {
					value := resource[member]
					if value == nil {
						if member == "path" {
							return "", errAssistantRunScope
						}
						continue
					}
					var path string
					if json.Unmarshal(value, &path) != nil {
						return "", errAssistantRunScope
					}
					if path == "" && member != "path" {
						continue
					}
					if err := observeResource(path, false); err != nil {
						return "", err
					}
				}
			}
		}
		if audit := doc["audit"]; audit != nil && string(audit) != "null" {
			var selection map[string]json.RawMessage
			if json.Unmarshal(audit, &selection) != nil || selection == nil {
				return "", errAssistantRunScope
			}
			for key := range selection {
				if !slices.Contains([]string{"dir", "chain", "signingKey", "signingPublicKey", "timestampAuthority"}, key) {
					return "", errAssistantRunScope
				}
			}
			var dir string
			if json.Unmarshal(selection["dir"], &dir) != nil || dir == "" {
				return "", errAssistantRunScope
			}
			if err := observeResource(dir, true); err != nil {
				return "", err
			}
			chained := true
			if selection["chain"] != nil {
				if json.Unmarshal(selection["chain"], &chained) != nil {
					return "", errAssistantRunScope
				}
			}
			if chained {
				signingKey = strings.TrimSpace(env[runtimeSigningKeyEnv])
			}
			if chained && signingKey == "" && selection["signingKey"] != nil {
				if json.Unmarshal(selection["signingKey"], &signingKey) != nil {
					return "", errAssistantRunScope
				}
			}
		}
		if err := observeResource("jpack.lock.json", false); err != nil {
			return "", err
		}
	}
	signingDigest := "absent"
	if signingKey != "" {
		if !filepath.IsAbs(signingKey) {
			return "", errAssistantRunScope
		}
		if err := record(filepath.Dir(signingKey), true); err != nil {
			return "", err
		}
		if err := record(signingKey, false); err != nil {
			return "", err
		}
		key, _, err := readAssistantRuntimeScopeFile(ctx, signingKey, 4096)
		if err != nil {
			return "", err
		}
		signingDigest = digestOf(key)
	}
	// Retain selection/environment only in this private digest preimage. No raw
	// configuration, key, bearer or environment is published as an owner label.
	preimage, err := json.Marshal(struct {
		Project, Config, ConfigIdentity, ConfigDigest, SigningDigest, SigningExpectation string
		Environment                                                                      []string
		Entries                                                                          []assistantRuntimeScopeEntry
	}{identityKey(held), config, configIdentity, configDigest, signingDigest, s.runtimeSigningExpectation(), environment, entries})
	if err != nil {
		return "", err
	}
	return digestOf(preimage), nil
}

func readAssistantRuntimeScopeFile(ctx context.Context, path string, limit int64) ([]byte, os.FileInfo, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errAssistantRunScope
	}
	data, err := io.ReadAll(io.LimitReader(assistantRuntimeArtifactReader{ctx: ctx, reader: file}, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, nil, errAssistantRunScope
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(info, current) || current.Size() != int64(len(data)) {
		return nil, nil, errAssistantRunScope
	}
	return data, info, nil
}

// Invocation settlement is supplied by the adapter owner after joining every
// callback and releasing the provider. Checkpoint status or context cancellation
// alone supplies none of these facts. Production adapters wire this in P3.
type assistantInvocationSettlement struct {
	PauseAcknowledged, ReceiptsAcknowledged          bool
	CallbacksJoined, ProviderReleased, NoPendingWork bool
	NativePauseCancellation                          bool
	// These are validation evidence for exact acknowledged native pauses,
	// never authority to dispatch or to recover an unknown effect.
	nativeOwner      *assistantRunExecution
	nativeGeneration uint64
	nativeBinding    *runcontext.RunBinding
	nativeClaims     []string
}

type assistantRuntimeContinuation func(context.Context, *runcontext.Input) error

type assistantInvocationGeneration struct {
	owner     *assistantRunExecution
	number    uint64
	finalizer bool
}
type assistantInvocationGenerationKey struct{}

func (e *assistantRunExecution) generationContext(ctx context.Context, n uint64) context.Context {
	return context.WithValue(ctx, assistantInvocationGenerationKey{}, assistantInvocationGeneration{owner: e, number: n})
}
func (e *assistantRunExecution) generationMatches(ctx context.Context, n uint64) bool {
	g, ok := ctx.Value(assistantInvocationGenerationKey{}).(assistantInvocationGeneration)
	return ok && g.owner == e && g.number == n
}
func (e *assistantRunExecution) settleRuntimeInvocation(ctx context.Context, proof assistantInvocationSettlement, resume assistantRuntimeContinuation, cleanup func()) error {
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if e.stopRequested || e.lifecycle != "running" || !e.generationMatches(ctx, e.generation) || resume == nil || cleanup == nil {
		return errAssistantRunScope
	}
	if e.gateNotices != nil && !e.gateNotices.parked(e.generation) {
		return errAssistantRunScope
	}
	runtime := e.runtimeOwner.Load()
	if e.retainedRuntime != nil && runtime != e.retainedRuntime {
		return errAssistantRunScope
	}
	if e.retainedRuntime == nil {
		e.retainedRuntime = runtime
	}
	e.lifecycle, e.settlement, e.resumeInvocation = "settling", &proof, resume
	if e.closeOwner == nil {
		e.closeOwner = cleanup
	}
	return nil
}
func (p *assistantInvocationSettlement) safe(err error) bool {
	return p != nil && p.PauseAcknowledged && p.ReceiptsAcknowledged && p.CallbacksJoined && p.ProviderReleased && p.NoPendingWork && (err == nil || p.NativePauseCancellation && errors.Is(err, context.Canceled))
}

// A positive adapter report must agree with the exact durable evidence. Native
// approval waiting receipt is allowed only for that exact claimed read-agent;
// unknown host/Runtime calls and lost receipts remain nonrecoverable.
func (e *assistantRunExecution) settledRuntimeCheckpoint(run assistantRunRecord) bool {
	e.canonicalMu.Lock()
	binding, proof, generation := e.canonicalBinding, e.settlement, e.generation
	e.canonicalMu.Unlock()
	if binding == nil || proof == nil || e.writer.Err() != nil || run.Checkpoint == nil || run.Checkpoint.Durable == nil || run.Checkpoint.Durable.PendingHandoff != nil || !sameAssistantRunCheckpoint(binding.Writer.Checkpoint(), run.Checkpoint) {
		return false
	}
	for _, op := range run.Operations {
		if op.State == "dispatched" || op.State == "uncertain" || op.State == "not-started" {
			return false
		}
	}
	if !proof.nativeApprovalRows(e, binding, generation, run.Checkpoint.Durable.Dispatches) {
		return false
	}
	for _, row := range run.Checkpoint.Durable.Dispatches {
		if row.State == "claimed" || row.Failure != nil && row.Failure.Detail != nil && strings.HasPrefix(*row.Failure.Detail, "ProviderReleaseUnknown:") {
			return false
		}
	}
	return e.runtimeContinuationCurrent(e.ctx, run.Checkpoint) == nil
}

// Only an exact claimed read-agent row observed by this live model's qualified
// approvalModel may explain the package's unknown Tool waiting receipt. Prior
// rows must remain byte-for-byte identical on every subsequent retained pause.
func (p *assistantInvocationSettlement) nativeApprovalRows(e *assistantRunExecution, binding *runcontext.RunBinding, generation uint64, rows []contract.CheckpointDurableDispatch) bool {
	if len(p.nativeClaims) > len(rows) || len(p.nativeClaims) != 0 && (p.nativeOwner != e || p.nativeBinding != binding || p.nativeGeneration != generation) {
		return false
	}
	claims := make(map[string]bool, len(p.nativeClaims))
	for _, claim := range p.nativeClaims {
		if _, duplicate := claims[claim]; duplicate {
			return false
		}
		claims[claim] = false
	}
	for _, row := range rows {
		if row.State != "unknown" {
			continue
		}
		// This exact generic failure is the released package's normalization
		// of the original Tool waiting error. Safety comes from the live proof
		// and original claim, never from this classification alone.
		if row.Kind != "agent" || !row.Read || row.Result != nil || len(row.Artifacts) != 0 || row.RepeatResultJSON != nil || row.PlanPause != nil || !reflect.DeepEqual(row.Failure, &contract.Failure{Version: 1, Code: "unknown", Source: "tool", Outcome: "unknown"}) {
			return false
		}
		row.State, row.Failure = "claimed", nil
		raw, err := json.Marshal(row)
		if err != nil {
			return false
		}
		claim := string(raw)
		if used, present := claims[claim]; !present || used {
			return false
		}
		claims[claim] = true
	}
	for _, used := range claims {
		if !used {
			return false
		}
	}
	return true
}

func (e *assistantRunExecution) runtimeContinuationCurrent(ctx context.Context, cp *contract.Checkpoint) error {
	e.canonicalMu.Lock()
	runtime := e.retainedRuntime
	e.canonicalMu.Unlock()
	if runtime == nil {
		if cp != nil && cp.Approval != nil && cp.Approval.Runtime != nil {
			return errAssistantRunScope
		}
		return nil // Explicit sourced-host fixture without a Runtime owner.
	}
	if runtime.owner == nil || !e.runtimeContinuationAlive(runtime) {
		return errAssistantRunScope
	}
	if cp != nil && cp.Approval != nil && cp.Approval.Runtime != nil && cp.Approval.Runtime.Owner != runtime.owner.identity {
		return errAssistantRunScope
	}
	_, err := runtime.owner.Current(ctx)
	return err
}
func (e *assistantRunExecution) runtimeContinuationAlive(runtime *assistantRuntime) bool {
	if runtime == nil || e.runtimeOwner.Load() != runtime || runtime.ctx.Err() != nil {
		return false
	}
	select {
	case <-runtime.waitDone:
		return false
	default:
		return true
	}
}

func assistantRunApprovalPause(run assistantRunRecord) bool {
	cp := run.Checkpoint
	return cp != nil && (cp.Status == "paused" || cp.Status == "stopped") && cp.Approval != nil && cp.Approval.State == "asked"
}

// runOwner owns the process lease until terminal cleanup. Each adapter invocation
// has a separate context and completion channel; canceling it never closes the
// owner. Service.mu only protects registry/notification, canonicalMu protects
// generation/installation. Neither is held across source I/O, Prepare or reaping.
func (e *assistantRunExecution) runOwner(_ context.Context, stopSession, stopPolicy func() bool, ownerCancel context.CancelFunc) {
	h, ctx, writer := e.service, e.ctx, e.writer
	phaseObserver := h.server.testAssistantRunPhase
	observePhase := func(phase string) {
		if phaseObserver != nil {
			phaseObserver(e, phase)
		}
	}
	defer func() {
		observePhase("done-close")
		close(e.done)
	}()
	defer stopSession()
	defer stopPolicy()
	defer ownerCancel()
	monitorChecked := h.server.testAssistantRunMonitorChecked
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.canonicalMu.Lock()
				retained := e.retainedRuntime
				e.canonicalMu.Unlock()
				if retained != nil && (!e.runtimeContinuationAlive(retained) || e.runtimeContinuationCurrent(ctx, nil) != nil) {
					e.cancel()
					return
				}
				if e.Authorize(ctx) != nil {
					e.cancel()
					return
				}
				if monitorChecked != nil {
					monitorChecked(ctx, e)
				}
			}
		}
	}()
	var next *runcontext.Input
	var runErr error
	for {
		e.canonicalMu.Lock()
		generation, resume := e.generation, e.resumeInvocation
		e.invocationDone = make(chan struct{})
		invocationDone := e.invocationDone
		e.canonicalMu.Unlock()
		invocation, cancel := context.WithCancel(e.generationContext(ctx, generation))
		runErr = e.Authorize(invocation)
		if runErr == nil {
			if next == nil {
				runErr = h.executor(e.source.Start.Purpose).Execute(invocation, e)
			} else {
				runErr = resume(invocation, next)
				observePhase("resume-return")
			}
		}
		cancel()
		close(invocationDone)
		bounded, finish := context.WithTimeout(context.Background(), 5*time.Second)
		observePhase("post-invocation-preview-entry")
		_ = e.saveProgressPreview(bounded, false)
		observePhase("post-invocation-preview-return")
		observePhase("post-invocation-read-entry")
		latest, readErr := writer.lease.store.Read(bounded, e.record.Binding, e.record.ID)
		observePhase("post-invocation-read-return")
		e.canonicalMu.Lock()
		canPark := readErr == nil && assistantCanonicalActive(latest) && assistantRunApprovalPause(latest) && e.lifecycle == "settling" && e.settlement.safe(runErr) && !e.stopRequested && ctx.Err() == nil
		e.canonicalMu.Unlock()
		if canPark && e.settledRuntimeCheckpoint(latest) && e.Authorize(bounded) == nil {
			if err := writer.State(bounded, "awaiting-input"); err == nil {
				e.canonicalMu.Lock()
				canPark = !e.stopRequested && ctx.Err() == nil
				if canPark {
					e.canonicalStore.ingress.Lock()
					e.canonicalStore.accept = false
					e.lifecycle = "parked"
					e.canonicalStore.ingress.Unlock()
				}
				e.canonicalMu.Unlock()
				if canPark {
					finish()
					h.notify(e.record.ID)
					select {
					case next = <-e.continuations:
						continue
					case <-ctx.Done():
					}
					break
				}
			}
		}
		// Nonretained ordinary pauses keep their supported envelope semantics.
		// An unqualified canonical ask is readable but cannot restart its owner.
		paused := runErr == nil && ctx.Err() == nil && readErr == nil && latest.Checkpoint != nil
		if readErr == nil && !assistantRunTerminal(latest.State) {
			var state string
			switch {
			case paused && assistantRunPlanReference(latest) != nil:
				state = "awaiting-input"
			case paused && assistantRunApprovalPause(latest):
				state = "awaiting-input"
			case paused && latest.State == "awaiting-client" && latest.Checkpoint.Status == "stopped":
			case paused && latest.Checkpoint.Status == "complete":
				if q, err := pendingAssistantRunQuestion(latest); err == nil && q != nil {
					state = "awaiting-input"
				} else {
					state = "complete"
				}
			case paused && latest.Checkpoint.Status == "paused":
				state = "paused-budget"
			default:
				runErr = errAssistantRunDispatch
			}
			if state != "" {
				if err := writer.State(bounded, state); err != nil {
					runErr = err
				}
			}
		}
		finish()
		break
	}
	h.mu.Lock()
	e.finalizing = true
	h.mu.Unlock()
	canceled := ctx.Err() != nil
	observePhase("runtime-cleanup-entry")
	e.cancel()
	<-monitorDone
	e.canonicalMu.Lock()
	cleanup, retained := e.closeOwner, e.retainedRuntime
	e.lifecycle = "closed"
	e.canonicalMu.Unlock()
	if cleanup != nil {
		cleanup()
	}
	if retained != nil {
		retained.Close()
	} else if runtime := e.runtimeOwner.Load(); runtime != nil {
		runtime.Close()
	}
	observePhase("runtime-cleanup-return")
	if canceled || runErr != nil {
		bounded, finish := context.WithTimeout(context.Background(), 5*time.Second)
		if e.finalizeCanonicalStop() == nil {
			_ = writer.Stop(bounded)
		} else if e.observation != nil {
			_ = writer.interruptObservation(bounded)
		}
		finish()
	}
	var finalRecord assistantRunRecord
	var finalReadErr error
	if e.contextHandle != nil {
		finalContext, finishContext := context.WithTimeout(context.Background(), 5*time.Second)
		observePhase("final-context-read-entry")
		finalRecord, finalReadErr = writer.lease.store.Read(finalContext, e.record.Binding, e.record.ID)
		finishContext()
		observePhase("final-context-read-return")
	}
	h.mu.Lock()
	h.finishContextLocked(e, finalRecord, finalReadErr, canceled || runErr != nil)
	delete(h.active, e.record.ID)
	h.notifyLocked(e.record.ID)
	h.releaseIdle()
	h.mu.Unlock()
	observePhase("active-removal-return")
}

type assistantRuntimeReservation struct {
	generation uint64
	binding    *runcontext.RunBinding
	head       assistantCanonicalHead
	store      *assistantCanonicalRunStore
	input      *runcontext.Input
}

// Called with service.mu held. A receipt is journaled once before preparation;
// every failed prepare/install closes this owner, so a duplicate never retries.
func (e *assistantRunExecution) reserveRuntimeContinuation(record assistantRunRecord) (*assistantRuntimeReservation, error) {
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if e.stopRequested || e.lifecycle != "parked" || e.canonicalBinding == nil || e.canonicalStore == nil || e.ctx.Err() != nil || record.Binding.Phase != "work" || !assistantCanonicalActive(record) || !assistantRunApprovalPause(record) {
		return nil, errAssistantRunUnavailable
	}
	e.lifecycle = "resuming"
	return &assistantRuntimeReservation{e.generation, e.canonicalBinding, record.Canonical.Head, e.canonicalStore, e.canonicalInput}, nil
}

// Prepare is read-only and deliberately outside lifecycle locks. Installation
// linearizes canonicalMu -> store.ingress -> Desk writer.mu -> lease.mu. Commit
// takes store.ingress -> writer.mu -> lease.mu and never calls canonicalMu.
// Package Checkpoint/Save/FinalizeStop and service notification stay outside.
func (e *assistantRunExecution) installRuntimeContinuation(ctx context.Context, reservation *assistantRuntimeReservation, prepared *runcontext.Input) error {
	if reservation == nil || prepared == nil || prepared.Persistence == nil || prepared.Persistence.Writer == nil {
		return errAssistantRunScope
	}
	if e.contextHandle != nil && (prepared.Chat != e.contextHandle.Chat() || reservation.input == nil || reservation.input.Chat != e.contextHandle.Chat()) {
		return errAssistantRunScope
	}
	b := prepared.Persistence
	cp := b.Writer.Checkpoint()
	if b.Writer.Store != reservation.store || b.Writer.ID != reservation.store.id || cp.Durable == nil || b.Writer.Failed() {
		return errAssistantRunScope
	}
	if err := e.Authorize(ctx); err != nil {
		return err
	}
	if err := e.runtimeContinuationCurrent(ctx, cp); err != nil {
		return err
	}
	latest, err := reservation.store.read(ctx)
	if err != nil || latest.Canonical == nil || !reflect.DeepEqual(latest.Canonical.Head, reservation.head) || !sameAssistantRunCheckpoint(cp, latest.Checkpoint) {
		return errAssistantRunConflict
	}
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	s := reservation.store
	s.ingress.Lock()
	defer s.ingress.Unlock()
	e.writer.mu.Lock()
	defer e.writer.mu.Unlock()
	e.writer.lease.mu.Lock()
	defer e.writer.lease.mu.Unlock()
	if e.stopRequested || e.ctx.Err() != nil || ctx.Err() != nil || e.lifecycle != "resuming" || e.generation != reservation.generation || e.canonicalBinding != reservation.binding || e.canonicalStore != s || e.writer.failure != nil || e.writer.lease.file == nil || e.writer.lease.recoveryErr != nil || e.writer.revision != latest.Revision || e.retainedRuntime != nil && !e.runtimeContinuationAlive(e.retainedRuntime) {
		return errAssistantRunScope
	}
	if e.gateNotices != nil && !e.gateNotices.parked(e.generation) {
		return errAssistantRunScope
	}
	e.generation++
	s.generation, s.accept = e.generation, true
	e.canonicalBinding = b
	e.lifecycle, e.settlement = "running", nil
	return nil
}

func (h *assistantRunService) continueRuntimeApproval(r *http.Request, record assistantRunRecord, action assistantRunAction, e *assistantRunExecution) (assistantRunRecord, error) {
	cp := record.Checkpoint
	if record.Binding.Phase != "work" || record.State != "awaiting-input" || !assistantRunApprovalPause(record) || record.Revision != action.Revision || cp.Approval.CallID != action.CallID || action.Approved == nil || !assistantRunApprovalMatches(cp.Approval, action) || action.Note != nil && runcontext.U16Length(*action.Note) > 2000 {
		return record, errAssistantRunConflict
	}
	reservation, err := e.reserveRuntimeContinuation(record)
	if err != nil {
		return record, err
	}
	decision := &contract.CheckpointApprovalDecision{CallID: action.CallID, Approved: *action.Approved, Note: action.Note, ShownDigest: action.ShownDigest}
	// No source I/O, public preparation, package writer lock or waits under h.mu.
	h.mu.Unlock()
	err = e.Authorize(r.Context())
	h.mu.Lock()
	if err != nil || h.closed || h.active[record.ID] != e {
		e.cancel()
		if err == nil {
			err = errAssistantRunScope
		}
		return record, err
	}
	err = e.writer.changeAt(r.Context(), false, &action.Revision, func(run *assistantRunRecord) (bool, error) {
		run.Actions = append(run.Actions, assistantRunActionReceipt{ID: action.ID, Digest: assistantRunActionDigest(action), Kind: action.Kind, Decision: decision})
		run.State = "admitted"
		return true, nil
	})
	if err != nil {
		e.cancel()
		return record, err
	}
	h.mu.Unlock()
	// No old Persistence/Checkpoint/Execution can short-circuit fresh Prepare.
	fresh := &runcontext.Input{RunStore: &runcontext.DurableRunInput{Store: reservation.store, ID: reservation.store.id, Resume: true}, Approval: decision}
	if reservation.input != nil {
		fresh.Chat = reservation.input.Chat
		fresh.Runtime = reservation.input.Runtime
		if reservation.input.RunStore != nil {
			fresh.RunStore.Context = reservation.input.RunStore.Context
		}
	}
	if e.contextHandle != nil && fresh.Chat != e.contextHandle.Chat() {
		e.cancel()
		h.mu.Lock()
		return record, errAssistantRunScope
	}
	prepared, err := runcontext.PrepareDurableInput(r.Context(), fresh)
	if err == nil {
		err = e.installRuntimeContinuation(r.Context(), reservation, prepared)
	}
	if err != nil {
		if prepared != nil && prepared.Persistence != nil {
			_ = prepared.Persistence.Writer.CloseDispatch(*prepared.Persistence)
		}
		e.cancel()
	} else {
		select {
		case e.continuations <- prepared:
		case <-e.ctx.Done():
			err = errAssistantRunScope
		}
	}
	h.mu.Lock()
	if err != nil {
		return record, err
	}
	h.notifyLocked(record.ID)
	return e.writer.lease.store.Read(r.Context(), record.Binding, record.ID)
}
