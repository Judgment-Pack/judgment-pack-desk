package desk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan"
)

func assistantExecutionID(kind string, n uint64) string {
	return "bound-" + kind + "-" + strconv.FormatUint(n, 10)
}

// A transport belongs to one held run and one rooted Runtime subprocess. Calls
// are serialized and receive host IDs, so independent child gates cannot alias
// the parent's RPC IDs. The adapter receives gate.Calls, never this transport.
type assistantRuntime struct {
	execution *assistantRunExecution
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *os.File
	waitDone  chan struct{}
	artifact  *assistantRuntimeArtifact
	owner     *assistantRuntimeOwner
	conn      *conn
	cancel    context.CancelFunc
	ctx       context.Context
	frames    chan json.RawMessage
	done      chan struct{}
	serial    chan struct{}
	once      sync.Once
}

func (e *assistantRunExecution) assistantRuntime(ctx context.Context) (*assistantRuntime, error) {
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	s := e.service.server
	lifetime, cancel := context.WithCancel(e.ctx)
	r := &assistantRuntime{execution: e, ctx: lifetime, cancel: cancel, frames: make(chan json.RawMessage), done: make(chan struct{}), waitDone: make(chan struct{}), serial: make(chan struct{}, 1)}
	r.conn = &conn{silent: true, out: make(chan []byte, 1), done: make(chan struct{}), cancel: cancel}
	if !s.register(r.conn) {
		cancel()
		return nil, errAssistantRunScope
	}
	fail := func(err error) (*assistantRuntime, error) {
		r.cancel()
		e.runtimeOwner.CompareAndSwap(r, nil)
		s.unregister(r.conn)
		if r.artifact != nil {
			_ = r.artifact.Close()
		}
		return nil, err
	}
	source, err := ResolveRuntime(s.cfg.JpackBin)
	if err != nil {
		return fail(err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	r.artifact, err = openSelectedAssistantRuntimeArtifact(ctx, source, executable, releaseplan.RuntimeArtifactSHA256())
	if err != nil {
		return fail(err)
	}
	project := heldDir{file: s.project.own.dirFile, path: s.projectDir, info: s.project.info}
	cmd, err := runtimeCommandAt(lifetime, r.artifact.path, project, "mcp")
	if err != nil {
		return fail(err)
	}
	r.cmd = cmd
	cmd.Env = s.runtimeEnv()
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	r.stdin, err = cmd.StdinPipe()
	if err != nil {
		return fail(err)
	}
	stdout, output, err := os.Pipe()
	if err != nil {
		_ = r.stdin.Close()
		return fail(err)
	}
	r.stdout = stdout
	cmd.Stdout = output
	if err := e.Authorize(ctx); err != nil {
		_ = r.stdin.Close()
		_ = stdout.Close()
		_ = output.Close()
		return fail(err)
	}
	// Keep the same final descriptor/path custody check as the WebSocket relay.
	if err := aimRuntimeAt(cmd, project); err != nil {
		_ = r.stdin.Close()
		_ = stdout.Close()
		_ = output.Close()
		return fail(err)
	}
	// Materialize after portable cwd selection so Cmd.Environ keeps its existing
	// PWD behavior as well as the actual config/signing environment selection.
	cmd.Env = cmd.Environ()
	// Capture these exact handles before publishing the transport. Stop must
	// never wait on a lease or canonical lock to interrupt blocked pipe I/O.
	r.cancel = assistantRuntimeCancellation(cancel, r.stdin, stdout)
	if !e.runtimeOwner.CompareAndSwap(nil, r) {
		_ = output.Close()
		r.cancel()
		return fail(errAssistantRunScope)
	}
	if !sameDirectory(s.projectDir, s.project.info) {
		_ = r.stdin.Close()
		_ = stdout.Close()
		_ = output.Close()
		return fail(errAssistantRunScope)
	}
	if err := cmd.Start(); err != nil {
		_ = r.stdin.Close()
		_ = stdout.Close()
		_ = output.Close()
		return fail(err)
	}
	_ = output.Close()
	// A separate wait owns process exit. Waiting never closes the reader's pipe;
	// cancellation closes both held streams without acquiring admission locks.
	stopOutput := context.AfterFunc(lifetime, r.cancel)
	go func() { _ = cmd.Wait(); close(r.waitDone); cancel() }()
	go func() {
		select {
		case <-r.conn.done:
			cancel()
		case <-lifetime.Done():
		}
	}()
	go func() {
		defer stopOutput()
		defer close(r.done)
		defer stdout.Close()
		defer func() { <-r.waitDone; _ = r.artifact.Close(); e.runtimeOwner.CompareAndSwap(r, nil) }()
		defer s.unregister(r.conn)
		defer r.conn.stop()
		defer cancel()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), readLimit)
		for scanner.Scan() {
			frame := bytes.Clone(scanner.Bytes())
			if len(bytes.TrimSpace(frame)) == 0 {
				continue
			}
			select {
			case r.frames <- frame:
			case <-lifetime.Done():
				_ = r.stdin.Close()
				return
			}
		}
		cancel()
		_ = r.stdin.Close()
	}()
	if !sameDirectory(s.projectDir, s.project.info) {
		r.Close()
		return nil, errAssistantRunScope
	}
	r.owner, err = newAssistantRuntimeOwner(ctx, r, cmd.Env)
	if err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// Close cancels and reaps the exact process started here, before returning.
func (r *assistantRuntime) Close() {
	r.once.Do(func() {
		r.cancel()
		_ = r.stdin.Close()
		<-r.done
	})
}

func (r *assistantRuntime) Call(ctx context.Context, message json.RawMessage) (json.RawMessage, error) {
	select {
	case r.serial <- struct{}{}:
		defer func() { <-r.serial }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, errAssistantRunDispatch
	}
	if err := r.execution.Authorize(ctx); err != nil {
		r.cancel()
		return nil, err
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	authority, err := holdAssistantRunPhysicalAuthority(r.execution)
	if err != nil {
		return nil, err
	}
	if !uniqueJSON(message) {
		return nil, errAssistantRunInvalid
	}
	var frame map[string]json.RawMessage
	if json.Unmarshal(message, &frame) != nil || frame == nil || string(frame["jsonrpc"]) != `"2.0"` {
		return nil, errAssistantRunInvalid
	}
	var method string
	if json.Unmarshal(frame["method"], &method) != nil || method == "" {
		return nil, errAssistantRunInvalid
	}
	originalID := bytes.Clone(frame["id"])
	if originalID != nil {
		var id any
		if json.Unmarshal(originalID, &id) != nil {
			return nil, errAssistantRunInvalid
		}
		switch id.(type) {
		case string, float64:
		default:
			return nil, errAssistantRunInvalid
		}
	}
	op, err := r.execution.boundIntent(ctx, "runtime", method, func(id string) ([]byte, error) {
		hostID, _ := json.Marshal(id)
		if originalID != nil {
			frame["id"] = hostID
		}
		outbound, err := json.Marshal(frame)
		if err != nil || len(outbound) > readLimit {
			return nil, errAssistantRunInvalid
		}
		return outbound, nil
	})
	if err != nil {
		return nil, err
	}
	// The fixed test-design host call may retain this existing journal receipt.
	// It cannot choose or replace the physical operation identity.
	if receipt, ok := ctx.Value(assistantTestRuntimeReceiptKey{}).(*assistantTestRuntimeReceipt); ok {
		receipt.ID = op.ID
	}
	outbound := op.Intent
	hostID, _ := json.Marshal(op.ID)
	s := r.execution.service.server
	if !s.admitRuntimeFrame(r.conn, outbound) {
		answer := <-r.conn.out
		if err := r.execution.Result(ctx, op.ID, "refused", answer); err != nil {
			return nil, err
		}
		return assistantRuntimeReply(answer, originalID)
	}
	if !s.beginProjectRequest(r.conn, outbound) {
		r.cancel()
		return nil, errAssistantRunScope
	}
	// Any cancellation after admission closes the whole transport. A late
	// response can never be mistaken for another call or safely replayed.
	stop := context.AfterFunc(ctx, r.cancel)
	defer stop()
	if err := r.execution.recheckAssistantCritic(ctx); err != nil {
		return nil, err
	}
	if err := recheckAssistantHost(ctx); err != nil {
		return nil, err
	}

	if err := r.execution.Dispatch(ctx, op.ID); err != nil {
		return nil, err
	}
	observed := false
	defer func() {
		if !observed {
			r.execution.cancel()
		}
	}()
	if r.owner != nil && r.owner.scopeErr == nil {
		if _, err := r.owner.Current(ctx); err != nil {
			r.cancel()
			return nil, err
		}
	} else if err := r.artifact.Current(); err != nil {
		r.cancel()
		return nil, err
	}
	if err := authority.write(ctx, func() error {
		if r.ctx.Err() != nil {
			return errAssistantRunScope
		}
		select {
		case <-r.waitDone:
			return errAssistantRunScope
		default:
		}
		if r.owner != nil && (r.cmd != r.owner.cmd || r.stdin != r.owner.stdin || r.stdout != r.owner.stdout || r.conn != r.owner.conn) {
			return errAssistantRunScope
		}
		_, err := r.stdin.Write(append(outbound, '\n'))
		return err
	}); err != nil {
		r.cancel()
		return nil, err
	}
	if originalID == nil {
		err := r.execution.Result(ctx, op.ID, "completed", []byte(`{"sent":true}`))
		observed = err == nil
		return nil, err
	}
	for range outBuffer {
		select {
		case answer := <-r.frames:
			if !uniqueJSON(answer) {
				r.cancel()
				return nil, errAssistantRunInvalid
			}
			var reply map[string]json.RawMessage
			if json.Unmarshal(answer, &reply) != nil || string(reply["jsonrpc"]) != `"2.0"` {
				r.cancel()
				return nil, errAssistantRunInvalid
			}
			if reply["method"] != nil && reply["id"] == nil {
				continue // bounded Runtime notifications, never tool replies
			}
			if !bytes.Equal(reply["id"], hostID) || (reply["result"] == nil) == (reply["error"] == nil) {
				r.cancel()
				return nil, errors.New("the Runtime did not answer the admitted request")
			}
			r.conn.finishProjectRequest(answer)
			if err := r.execution.Result(ctx, op.ID, "completed", answer); err != nil {
				return nil, err
			}
			observed = true
			return assistantRuntimeReply(answer, originalID)
		case <-ctx.Done():
			r.cancel()
			return nil, ctx.Err()
		case <-r.done:
			return nil, fmt.Errorf("the Runtime stopped before its admitted response")
		}
	}
	r.cancel()
	return nil, errors.New("the Runtime sent too many messages before its response")
}

func assistantRuntimeReply(answer, originalID json.RawMessage) (json.RawMessage, error) {
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(answer, &frame); err != nil {
		return nil, err
	}
	frame["id"] = originalID
	return json.Marshal(frame)
}

// Capture immutable stream handles so even refused retargeting cannot cause
// cancellation to close a replacement object. Repeated cancellation is safe.
func assistantRuntimeCancellation(cancel context.CancelFunc, stdin io.WriteCloser, stdout io.Closer) context.CancelFunc {
	return func() { cancel(); _ = stdin.Close(); _ = stdout.Close() }
}
