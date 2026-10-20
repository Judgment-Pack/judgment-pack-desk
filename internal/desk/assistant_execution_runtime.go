package desk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
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
	r := &assistantRuntime{execution: e, ctx: lifetime, cancel: cancel, frames: make(chan json.RawMessage), done: make(chan struct{}), serial: make(chan struct{}, 1)}
	r.conn = &conn{silent: true, out: make(chan []byte, 1), done: make(chan struct{}), cancel: cancel}
	if !s.register(r.conn) {
		cancel()
		return nil, errAssistantRunScope
	}
	fail := func(err error) (*assistantRuntime, error) {
		cancel()
		s.unregister(r.conn)
		return nil, err
	}
	cmd, err := s.runtimeCommand(lifetime)
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = r.stdin.Close()
		return fail(err)
	}
	if err := e.Authorize(ctx); err != nil {
		_ = r.stdin.Close()
		_ = stdout.Close()
		return fail(err)
	}
	// Keep the same final descriptor/path custody check as the WebSocket relay.
	if err := s.aimAtTheProject(cmd); err != nil {
		_ = r.stdin.Close()
		_ = stdout.Close()
		return fail(err)
	}
	if err := cmd.Start(); err != nil {
		_ = r.stdin.Close()
		_ = stdout.Close()
		return fail(err)
	}
	stopOutput := context.AfterFunc(lifetime, func() { _ = stdout.Close() })
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
				_ = cmd.Wait()
				return
			}
		}
		cancel()
		_ = r.stdin.Close()
		_ = cmd.Wait()
	}()
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
	if err := r.execution.Dispatch(ctx, op.ID); err != nil {
		return nil, err
	}
	observed := false
	defer func() {
		if !observed {
			r.execution.cancel()
		}
	}()
	if _, err := r.stdin.Write(append(outbound, '\n')); err != nil {
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
