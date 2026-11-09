package desk

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

// A copied test executable provides both the Linux trampoline and the Runtime
// protocol peer. It enters here before testing flags are parsed. No installed
// shell, interpreter, compiler, Runtime or provider executable is launched.
func init() {
	if os.Getenv("JPS_RUNTIME_OWNER_P1_PEER") != "1" {
		return
	}
	if len(os.Args) == 5 && os.Args[1] == "-c" {
		dir := os.NewFile(3, "fixture-project")
		if dir == nil || dir.Chdir() != nil {
			os.Exit(91)
		}
		_ = dir.Close()
		if syscall.Exec(os.Args[3], os.Args[3:], os.Environ()) != nil {
			os.Exit(92)
		}
		os.Exit(0)
	}
	if len(os.Args) != 2 || os.Args[1] != "mcp" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), readLimit)
	for scanner.Scan() {
		var q struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &q) != nil {
			os.Exit(93)
		}
		if q.Method == "fixture/block" {
			for {
				time.Sleep(time.Hour)
			}
		}
		if q.Method == "fixture/exit" {
			os.Exit(0)
		}
		if len(q.ID) == 0 {
			continue
		}
		var result any
		switch q.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "owned-peer", "version": "fixture"}}
		case "tools/list":
			result = map[string]any{"tools": []any{
				map[string]any{"name": "get_schema", "description": "Read fixture schema", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}, "_meta": map[string]any{"operation": "read", "effect": "read"}},
				map[string]any{"name": "experimental_evaluate", "description": "Evaluate exact fixture draft", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"rehearsal": map[string]string{"type": "boolean"}}}, "_meta": map[string]any{"operation": "evaluate", "effect": "act", "experimental": true}},
			}}
		case "prompts/list":
			result = map[string]any{"prompts": []any{map[string]any{"name": "served", "arguments": []any{}}}}
		case "prompts/get":
			result = map[string]any{"messages": []any{map[string]any{"role": "user", "content": map[string]string{"type": "text", "text": "Review exact fixture input."}}}}
		default:
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(q.Params, &p)
			if q.Method == "tools/call" && p.Name == "experimental_evaluate" {
				// Keep the default exact result independent of cwd. The guarded
				// fixture explicitly selects public text matched by the guard.
				text := "Exact fixture evaluation result."
				if os.Getenv("JPS_RUNTIME_OWNER_P1_RESULT") == "guarded-public-uri" {
					text = "Evaluation result for https://example.invalid/tmp/public-policy"
				}
				result = map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}}
				break
			}
			cwd, _ := os.Getwd()
			text, _ := json.Marshal(map[string]any{"cwd": cwd, "name": p.Name, "arguments": p.Arguments})
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(text)}}}
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
		if _, err := fmt.Fprintln(os.Stdout, string(response)); err != nil {
			os.Exit(94)
		}
	}
	os.Exit(0)
}

func assistantP1Hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func assistantP1Executable(t *testing.T, path string, raw []byte) {
	t.Helper()
	assistantRunMust(t, os.WriteFile(path, raw, 0700))
}

func TestAssistantRuntimeOwnerArtifactTrustClassification(t *testing.T) {
	for _, scenario := range []string{"bundled", "desk symlink", "runtime symlink", "hard link", "replaced bundled path", "external override", "source build", "bundled mismatch", "runtime symlink mismatch", "hard link mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			executable := filepath.Join(dir, "jpack-desk")
			source := filepath.Join(dir, "jpack")
			assistantP1Executable(t, executable, []byte("desk fixture"))
			assistantP1Executable(t, source, []byte("abc"))
			releaseDigest := assistantRuntimeArtifactFixtureSHA256
			switch scenario {
			case "desk symlink":
				alias := filepath.Join(t.TempDir(), "desk-link")
				assistantRunMust(t, os.Symlink(executable, alias))
				executable = alias
			case "runtime symlink", "runtime symlink mismatch":
				alias := filepath.Join(t.TempDir(), "runtime-link")
				assistantRunMust(t, os.Symlink(source, alias))
				source = alias
			case "hard link", "hard link mismatch":
				alias := filepath.Join(t.TempDir(), "runtime-link")
				assistantRunMust(t, os.Link(source, alias))
				source = alias
			case "replaced bundled path":
				assistantRunMust(t, os.Rename(source, source+".old"))
				assistantP1Executable(t, source, []byte("changed"))
			case "external override":
				source = filepath.Join(t.TempDir(), "operator-runtime")
				assistantP1Executable(t, source, []byte("local override"))
			case "source build":
				releaseDigest = ""
				assistantP1Executable(t, source, []byte("local build"))
			}
			if strings.HasSuffix(scenario, "mismatch") {
				assistantP1Executable(t, source, []byte("mismatched bytes"))
			}
			held, err := os.Open(source)
			assistantRunMust(t, err)
			selection, err := selectAssistantRuntimeArtifact(source, held, executable, releaseDigest)
			_ = held.Close()
			assistantRunMust(t, err)
			bundled := scenario != "external override" && scenario != "source build"
			if selection.bundled != bundled || bundled && selection.expectedDigest != releaseDigest {
				t.Fatal("wrong host trust classification", selection)
			}
			artifact, err := openSelectedAssistantRuntimeArtifact(t.Context(), source, executable, releaseDigest)
			if scenario == "replaced bundled path" || strings.HasSuffix(scenario, "mismatch") {
				if err == nil || artifact != nil {
					t.Fatal("mismatched bundle fell back to local trust")
				}
				return
			}
			assistantRunMust(t, err)
			defer artifact.Close()
			assistantRunMust(t, artifact.Current())
			raw, err := os.ReadFile(source)
			assistantRunMust(t, err)
			if artifact.digest != assistantP1Hash(raw) {
				t.Fatal("private bytes did not identify selected artifact")
			}
		})
	}
}

func TestAssistantRuntimeOwnerHeldDescriptorReplacement(t *testing.T) {
	source := runtimeArtifactFixture(t)
	held, err := os.Open(source)
	assistantRunMust(t, err)
	defer held.Close()
	assistantRunMust(t, os.Rename(source, source+".original"))
	assistantP1Executable(t, source, []byte("path replacement"))
	artifact, err := holdAssistantRuntimeArtifact(t.Context(), held, assistantRuntimeArtifactFixtureSHA256)
	assistantRunMust(t, err)
	defer artifact.Close()
	data, err := os.ReadFile(artifact.path)
	assistantRunMust(t, err)
	if string(data) != "abc" {
		t.Fatal("copy re-resolved the replaced source path")
	}
}

func TestAssistantRuntimeOwnerAmbiguousDeploymentRefuses(t *testing.T) {
	source := runtimeArtifactFixture(t)
	if artifact, err := openSelectedAssistantRuntimeArtifact(t.Context(), source, filepath.Join(t.TempDir(), "missing-desk"), assistantRuntimeArtifactFixtureSHA256); err == nil || artifact != nil {
		t.Fatal("ambiguous bundle deployment silently became local trust")
	}
}

func assistantP1RuntimeFixture(t *testing.T, canonical bool, configure ...func(*Server)) (*Server, string, *assistantRunExecution, *assistantRuntime) {
	t.Helper()
	s, token, e := assistantP1ExecutionFixture(t, canonical, configure...)
	r, err := e.assistantRuntime(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(r.Close)
	return s, token, e, r
}

// Configure the test-owned executable before the production opener captures
// critic custody and starts its first Runtime. Runtime discovery itself journals
// operations, so it cannot precede that initial custody capture.
func assistantP1ExecutionFixture(t *testing.T, canonical bool, configure ...func(*Server)) (*Server, string, *assistantRunExecution) {
	t.Helper()
	prepare := func(s *Server, _ string, start assistantRunStart) assistantRunStart {
		if canonical {
			s.assistantRunExecutor = &assistantCanonicalProtocolExecutor{s.assistantRunExecutor.(*assistantProtocolExecutor)}
		}
		return start
	}
	s, token, e := assistantExecutionFixtureSource(t, "http://127.0.0.1:1", "work", nil, prepare)
	exe, err := os.Executable()
	assistantRunMust(t, err)
	raw, err := os.ReadFile(exe)
	assistantRunMust(t, err)
	binaries := t.TempDir()
	bin := filepath.Join(binaries, "jpack")
	assistantP1Executable(t, bin, raw)
	assistantP1Executable(t, filepath.Join(binaries, "sh"), raw)
	t.Setenv("PATH", binaries)
	t.Setenv("JPS_RUNTIME_OWNER_P1_PEER", "1")
	s.cfg.JpackBin = bin
	t.Setenv(runtimeConfigEnv, "")
	t.Setenv(runtimeSigningKeyEnv, "")
	t.Setenv(runtimeSigningPublicKeyEnv, "")
	assistantRunMust(t, os.Mkdir(filepath.Join(s.projectDir, "audit"), 0700))
	assistantRunMust(t, os.Mkdir(filepath.Join(s.projectDir, "resources"), 0700))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "resources", "fixture.json"), []byte(`{"fixture":true}`), 0600))
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "jpack.json"), []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{"fixture":{"path":"resources/fixture.json"}}}`), 0600))
	for _, change := range configure {
		change(s)
	}
	return s, token, e
}

func TestAssistantRuntimeOwnerActualCurrentAndSourceReplacement(t *testing.T) {
	s, _, _, r := assistantP1RuntimeFixture(t, true)
	original, err := r.owner.Current(t.Context())
	assistantRunMust(t, err)
	if original.ProcessID == "" || original.SessionID == "" || r.owner.port() == nil || original.ArtifactDigest != "sha256:"+r.artifact.digest {
		t.Fatal("actual retained port missing")
	}
	assistantRunMust(t, os.Rename(s.cfg.JpackBin, s.cfg.JpackBin+".old"))
	assistantP1Executable(t, s.cfg.JpackBin, []byte("replacement"))
	current, err := r.owner.Current(t.Context())
	assistantRunMust(t, err)
	if current != original {
		t.Fatal("installation update retargeted owner")
	}
	reply, err := r.rpc(t.Context(), "tools/call", map[string]any{"name": "get_schema", "arguments": map[string]any{}})
	assistantRunMust(t, err)
	if !bytes.Contains(reply, []byte("get_schema")) {
		t.Fatal("held Runtime did not answer after source replacement")
	}
	artifactPath := r.artifact.path
	r.Close()
	if _, err := r.owner.Current(t.Context()); err == nil {
		t.Fatal("reaped owner remained current")
	}
	if _, err := os.Lstat(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("private artifact retained after exact process reaped", err)
	}
}

func TestAssistantRuntimeOwnerActualDriftRefuses(t *testing.T) {
	for _, kind := range []string{"configuration", "resource root", "audit contents stay mutable", "session", "transport", "lease", "project", "process exit"} {
		if !t.Run(kind, func(t *testing.T) {
			s, token, e, r := assistantP1RuntimeFixture(t, true)
			initial, err := r.owner.Current(t.Context())
			assistantRunMust(t, err)
			switch kind {
			case "configuration":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "jpack.json"), []byte(`{"configVersion":"3","packs":{}}`), 0600))
			case "resource root":
				path := filepath.Join(s.projectDir, "resources")
				assistantRunMust(t, os.Rename(path, path+".old"))
				assistantRunMust(t, os.Mkdir(path, 0700))
				assistantRunMust(t, os.WriteFile(filepath.Join(path, "fixture.json"), []byte(`{"fixture":true}`), 0600))
			case "audit contents stay mutable":
				assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "audit", "record.json"), []byte(`{"observed":true}`), 0600))
			case "session":
				s.sessions.revoke(token)
			case "transport":
				original := r.stdin
				read, write, err := os.Pipe()
				assistantRunMust(t, err)
				r.stdin = write
				defer func() { r.stdin = original; _ = read.Close(); _ = write.Close() }()
			case "lease":
				assistantRunMust(t, e.writer.lease.Close())
			case "project":
				original := s.projectDir
				s.projectDir = filepath.Join(original, "missing")
				defer func() { s.projectDir = original }()
			case "process exit":
				_, _ = r.rpc(t.Context(), "fixture/exit", map[string]any{})
				select {
				case <-r.waitDone:
				case <-time.After(3 * time.Second):
					t.Fatal("owned process did not exit")
				}
			}
			current, err := r.owner.Current(t.Context())
			if kind == "audit contents stay mutable" {
				assistantRunMust(t, err)
				if current != initial {
					t.Fatal("mutable audit contents changed authority")
				}
			} else if err == nil {
				t.Fatal("owner accepted drift", kind)
			}
		}) {
			return
		}
	}
}

func TestAssistantRuntimeOwnerPhysicalAdmission(t *testing.T) {
	for _, kind := range []string{"live", "closed lease", "Stop", "changed binding"} {
		t.Run(kind, func(t *testing.T) {
			_, _, e, r := assistantP1RuntimeFixture(t, true)
			authority, err := holdAssistantRunPhysicalAuthority(e)
			assistantRunMust(t, err)
			switch kind {
			case "closed lease":
				assistantRunMust(t, e.writer.lease.Close())
			case "Stop":
				e.closeCanonicalDispatch()
			case "changed binding":
				e.canonicalMu.Lock()
				old := e.canonicalBinding
				copy := *old
				e.canonicalBinding = &copy
				e.canonicalMu.Unlock()
			}
			sent := false
			err = authority.write(t.Context(), func() error { sent = true; return nil })
			if kind == "live" {
				assistantRunMust(t, err)
				if !sent {
					t.Fatal("live admission refused")
				}
			} else if err == nil || sent {
				t.Fatal("invalid physical admission won", kind, err)
			}
			_ = r
		})
	}
}

func TestAssistantRuntimeOwnerBlockedWriteCancellation(t *testing.T) {
	_, _, e, r := assistantP1RuntimeFixture(t, true)
	authority, err := holdAssistantRunPhysicalAuthority(e)
	assistantRunMust(t, err)
	// Stop the test-owned peer's reader, then fill the actual held stdin. No
	// transport replacement or local stand-in guard is used for this fence.
	_, err = r.Call(t.Context(), json.RawMessage(`{"jsonrpc":"2.0","method":"fixture/block"}`))
	assistantRunMust(t, err)

	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- authority.write(t.Context(), func() error { close(entered); _, err := r.stdin.Write(make([]byte, 2<<20)); return err })
	}()
	<-entered
	e.cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled blocked write completed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked physical write prevented Stop cancellation")
	}
}

func TestAssistantRuntimeOwnerStartupScopeSelection(t *testing.T) {
	s, _, _, r := assistantP1RuntimeFixture(t, true)
	override := filepath.Join(t.TempDir(), "selected.json")
	assistantRunMust(t, os.WriteFile(override, []byte(`{"configVersion":"1","packs":{}}`), 0600))
	environment := append([]string{}, r.owner.environment...)
	for i, value := range environment {
		if strings.HasPrefix(value, runtimeConfigEnv+"=") {
			environment[i] = runtimeConfigEnv + "=" + override
		}
	}
	first, err := observeAssistantRuntimeScope(t.Context(), s, environment)
	assistantRunMust(t, err)
	assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "jpack.json"), []byte(`{"configVersion":"1","packs":{}}`), 0600))
	second, err := observeAssistantRuntimeScope(t.Context(), s, environment)
	assistantRunMust(t, err)
	if first != second {
		t.Fatal("startup override borrowed project configuration")
	}
	assistantRunMust(t, os.WriteFile(override, []byte(`{"configVersion":"2","packs":{}}`), 0600))
	third, err := observeAssistantRuntimeScope(t.Context(), s, environment)
	assistantRunMust(t, err)
	if second == third {
		t.Fatal("effective override drift was not observed")
	}
}

func TestAssistantRuntimeOwnerActualGatePortDefaultsToAsk(t *testing.T) {
	_, _, e := assistantP1ExecutionFixture(t, true)
	// Open through the production seam before any Runtime operations. Its
	// initial critic custody must precede discovery, exactly as in production.
	x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	defer x.Close()
	if gate.RuntimeActionOwner(x.gate.Calls()) == nil || x.runtime != e.runtimeOwner.Load() {
		t.Fatal("production open omitted actual retained Runtime owner")
	}
	acting := false
	for _, tool := range x.Run.Tools {
		if tool.Name == "experimental_evaluate" && tool.Meta.Effect == "act" {
			acting = true
		}
	}
	if !acting {
		t.Fatal("production open omitted actual acting offer")
	}
	// Observe the actual adapter without replacing canonical journal delivery.
	// A nil adapter result alone does not establish that context or a model opened.
	var eventMu sync.Mutex
	eventCount := 0
	originalEmit := x.Run.Emit
	x.Run.Emit = func(event contract.RunEvent) {
		eventMu.Lock()
		eventCount++
		if eventCount <= 16 {
			raw, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				t.Logf("diagnostic event=%d marshal=%v", eventCount, marshalErr)
			} else {
				shown := raw
				if len(shown) > 4096 {
					shown = shown[:4096]
				}
				t.Logf("diagnostic event=%d bytes=%d sha256=%s prefix=%s", eventCount, len(raw), assistantP1Hash(raw), shown)
			}
		}
		eventMu.Unlock()
		originalEmit(event)
	}
	declarations := []contract.ToolOfferHostTool{}
	for _, tool := range x.Run.HostTools {
		declaration := tool.Declaration()
		declarations = append(declarations, declaration)
		raw, marshalErr := json.Marshal(declaration)
		assistantRunMust(t, marshalErr)
		t.Logf("diagnostic declaration name=%s bytes=%d schemaBytes=%d sha256=%s", tool.Name, len(raw), len(declaration.InputSchema), assistantP1Hash(raw))
	}
	catalog, err := json.Marshal(declarations)
	assistantRunMust(t, err)
	t.Logf("diagnostic full initial declarations=%d bytes=%d sha256=%s", len(declarations), len(catalog), assistantP1Hash(catalog))
	logCheckpoint := func(label string, cp *contract.Checkpoint) {
		if cp == nil {
			t.Logf("diagnostic checkpoint %s=nil", label)
			return
		}
		raw, marshalErr := json.Marshal(cp)
		assistantRunMust(t, marshalErr)
		t.Logf("diagnostic checkpoint %s id=%s requestKey=%s status=%s used=%d approval=%t bytes=%d sha256=%s", label, cp.ID, cp.RequestKey, cp.Status, cp.Used, cp.Approval != nil, len(raw), assistantP1Hash(raw))
	}
	logCheckpoint("context-before", x.Run.Context.Execution)
	logCheckpoint("persisted-before", assistantExecutionRecord(t, e).Checkpoint)
	heldBinding := x.Run.Context.Persistence
	t.Logf("diagnostic binding root=%t sameOriginal=%t scope=%+v", heldBinding.Root, heldBinding == e.canonicalBinding, heldBinding.Scope)
	calls := 0
	x.Run.Model = func(context.Context, string, relay.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("unexpected provider request after held ask")
		}
		recorder := httptest.NewRecorder()
		assistantPolicySSE(recorder, "experimental_evaluate", json.RawMessage(`{"rehearsal":false}`), "")
		return recorder.Result(), nil
	}
	adapterErr := x.Adapter.Run(t.Context(), x.Run)
	record := assistantExecutionRecord(t, e)
	logCheckpoint("context-after", x.Run.Context.Execution)
	logCheckpoint("persisted-original-after", record.Checkpoint)
	eventMu.Lock()
	t.Logf("diagnostic adapterErr=%v modelCalls=%d events=%d sameOriginalBinding=%t sameOriginalWriter=%t", adapterErr, calls, eventCount, x.Run.Context.Persistence == heldBinding && e.canonicalBinding == heldBinding, x.Run.Context.Persistence.Writer == heldBinding.Writer)
	eventMu.Unlock()
	assistantRunMust(t, adapterErr)
	if calls != 1 {
		t.Fatalf("actual default ask did not reach exactly one synthetic model callback: %d", calls)
	}
	if record.Checkpoint == nil || record.Checkpoint.Approval == nil || record.Checkpoint.Approval.State != "asked" || record.Checkpoint.Approval.Runtime == nil || record.Checkpoint.Status != "paused" {
		t.Fatal("actual port did not preserve default ask", record.Checkpoint)
	}
	args, _ := json.Marshal(record.Checkpoint.Approval.Args)
	if bytes.Contains(args, []byte(`"rehearsal":false`)) {
		t.Fatal("Runtime ask lost rehearsal rewrite")
	}
}

// This uses the real admitted observation store because its Commit takes the
// exact canonicalMu -> writer.mu -> lease.mu chain discovered in production.
// Pipes are test-owned; this is lock/admission evidence, not native launch proof.
func TestAssistantRuntimeOwnerBlockedWriteCompetingCommitStop(t *testing.T) {
	f := newObservationStoreFixture(t)
	opened, err := runcontext.ContextualSession(f.e.ctx, observationFixtureSession(f.input(t)))
	assistantRunMust(t, err)
	defer opened.Context.Close()
	saved := f.saved(t)
	authority, err := holdAssistantRunPhysicalAuthority(f.e)
	assistantRunMust(t, err)
	read, write, err := os.Pipe()
	assistantRunMust(t, err)
	defer read.Close()
	defer write.Close()
	stdout, output, err := os.Pipe()
	assistantRunMust(t, err)
	defer stdout.Close()
	defer output.Close()
	lifetime, cancel := context.WithCancel(f.e.ctx)
	r := &assistantRuntime{ctx: lifetime, stdin: write, stdout: stdout}
	r.cancel = assistantRuntimeCancellation(cancel, write, stdout)
	defer r.cancel()
	if !f.e.runtimeOwner.CompareAndSwap(nil, r) {
		t.Fatal("fixture replaced a retained Runtime")
	}
	defer f.e.runtimeOwner.CompareAndSwap(r, nil)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- authority.write(t.Context(), func() error { _, err := write.Write(make([]byte, 2<<20)); return err })
	}()
	readable := make(chan error, 1)
	go func() { one := make([]byte, 1); _, err := read.Read(one); readable <- err }()
	select {
	case err := <-readable:
		assistantRunMust(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("physical write did not enter test-owned pipe")
	}
	// Hold writer.mu while the REAL Commit acquires canonicalMu, then release it
	// into the lease wait behind the blocked physical write. No local live flag.
	f.writer.mu.Lock()
	commitDone := make(chan error, 1)
	go func() {
		_, err := f.e.observation.store.Commit(t.Context(), runcontext.RunCommit{RunID: saved.Canonical.Head.RunID, ExpectedRevision: saved.Canonical.Head.Revision, CommitID: "competing-blocked-write", Checkpoint: saved.Canonical.Head.Checkpoint, Events: [][]byte{}})
		commitDone <- err
	}()
	acquired := false
	for n := 0; n < 3000; n++ {
		if !f.e.canonicalMu.TryLock() {
			acquired = true
			break
		}
		f.e.canonicalMu.Unlock()
		time.Sleep(time.Millisecond)
	}
	f.writer.mu.Unlock()
	if !acquired {
		t.Fatal("actual competing Commit never acquired canonicalMu")
	}
	stopDone := make(chan struct{})
	go func() { f.e.cancel(); close(stopDone) }()
	select {
	case <-stopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("real Stop deadlocked behind Commit and blocked physical write")
	}
	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("blocked write did not report interruption")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not interrupt the exact held stdin")
	}
	select {
	case <-commitDone:
	case <-time.After(3 * time.Second):
		t.Fatal("competing Commit remained blocked after cancellation")
	}
	if !f.e.stopRequested || f.e.ctx.Err() == nil {
		t.Fatal("real cancellation did not close canonical admission")
	}
}

func TestAssistantRuntimeOwnerUnavailableScopeKeepsReadPath(t *testing.T) {
	for _, kind := range []string{"malformed", "nonregular", "unknown configuration"} {
		t.Run(kind, func(t *testing.T) {
			_, _, _, r := assistantP1RuntimeFixture(t, true, func(s *Server) {
				path := filepath.Join(s.projectDir, "jpack.json")
				switch kind {
				case "malformed":
					assistantRunMust(t, os.WriteFile(path, []byte(`{`), 0600))
				case "nonregular":
					assistantRunMust(t, os.Remove(path))
					assistantRunMust(t, syscall.Mkfifo(path, 0600))
				case "unknown configuration":
					assistantRunMust(t, os.WriteFile(path, []byte(`{"configVersion":"future","packs":{}}`), 0600))
				}
			})
			if r.owner.scopeErr == nil || r.owner.port() != nil {
				t.Fatal("unobserved scope obtained an acting port")
			}
			if _, err := r.rpc(t.Context(), "tools/list", map[string]any{}); err != nil {
				t.Fatal("scope refusal disabled existing read transport", err)
			}
		})
	}
}

func TestAssistantRuntimeOwnerScopeFileBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded")
	for _, raw := range []string{"abc", "abcd"} {
		assistantRunMust(t, os.WriteFile(path, []byte(raw), 0600))
		data, _, err := readAssistantRuntimeScopeFile(t.Context(), path, 3)
		if len(raw) == 3 {
			assistantRunMust(t, err)
			if string(data) != raw {
				t.Fatal("bounded bytes changed")
			}
		} else if err == nil {
			t.Fatal("one-over scope file was accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := readAssistantRuntimeScopeFile(ctx, path, 3); err == nil {
		t.Fatal("cancelled observation accepted bytes")
	}
}

func TestAssistantRuntimeOwnerBundledHeldAliasReplacement(t *testing.T) {
	dir := t.TempDir()
	desk := filepath.Join(dir, "jpack-desk")
	bundle := filepath.Join(dir, "jpack")
	alias := filepath.Join(dir, "alias")
	assistantP1Executable(t, desk, []byte("desk"))
	assistantP1Executable(t, bundle, []byte("abc"))
	assistantRunMust(t, os.Link(bundle, alias))
	held, err := os.Open(alias)
	assistantRunMust(t, err)
	defer held.Close()
	selection, err := selectAssistantRuntimeArtifact(alias, held, desk, assistantRuntimeArtifactFixtureSHA256)
	assistantRunMust(t, err)
	if !selection.bundled {
		t.Fatal("held bundled alias lost provenance")
	}
	assistantRunMust(t, os.Rename(alias, alias+".original"))
	assistantP1Executable(t, alias, []byte("replacement"))
	artifact, err := holdAssistantRuntimeArtifact(t.Context(), held, selection.expectedDigest)
	assistantRunMust(t, err)
	defer artifact.Close()
	bytes, err := os.ReadFile(artifact.path)
	assistantRunMust(t, err)
	if string(bytes) != "abc" || artifact.digest != assistantRuntimeArtifactFixtureSHA256 {
		t.Fatal("path update reselected bytes or downgraded bundle")
	}
}
