// Package codexbridge implements Desk's private, version-pinned Codex protocol.
// The managed runtime is prepared on explicit subscription connection.
package codexbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const Version = "codex-cli 0.156.0"
const maxFrame = 4 << 20

var (
	ErrUnavailable = errors.New("Codex is unavailable or its protocol is incompatible")
	ErrUpstream    = errors.New("Codex could not complete the request")
	ErrBusy        = errors.New("another Codex operation is active")
)

type frame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// client owns exactly one process. Raw frames and stderr never leave this package.
// Closing it interrupts pending requests and reaps the process before returning.
type client struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan frame
	events  chan frame
	done    chan struct{}
	exited  chan struct{}
	once    sync.Once
	next    atomic.Uint64
}

func startClient(ctx context.Context, cmd *exec.Cmd) (*client, error) {
	if err := prepareProcess(cmd); err != nil {
		return nil, err
	}
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, ErrUnavailable
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, ErrUnavailable
	}
	c := &client{cmd: cmd, input: input, output: output, pending: make(map[string]chan frame), events: make(chan frame, 64), done: make(chan struct{}), exited: make(chan struct{})}
	go func() {
		defer close(c.exited)
		scan := bufio.NewScanner(output)
		scan.Buffer(make([]byte, 4096), maxFrame)
		for scan.Scan() {
			var f frame
			if json.Unmarshal(scan.Bytes(), &f) != nil {
				break
			}
			if f.Method == "" && len(f.ID) > 0 {
				c.mu.Lock()
				waiter := c.pending[string(f.ID)]
				delete(c.pending, string(f.ID))
				c.mu.Unlock()
				if waiter == nil || (len(f.Result) == 0 && len(f.Error) == 0) {
					break
				}
				waiter <- f
			} else {
				if f.Method == "" {
					break
				}
				select {
				case c.events <- f:
				case <-c.done:
					goto finished
				default:
					goto finished
				}
			}
		}
	finished:
		c.stop()
		_ = cmd.Wait()
	}()
	var hello struct {
		UserAgent string `json:"userAgent"`
	}
	if err := c.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "jps_desk", "version": "0.1.0"}, "capabilities": map[string]any{"experimentalApi": true}}, &hello); err != nil {
		c.close()
		return nil, err
	}
	if !strings.HasPrefix(hello.UserAgent, "jps_desk/0.156.0 (") {
		c.close()
		return nil, ErrUnavailable
	}
	if err := c.send(map[string]any{"method": "initialized"}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *client) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxFrame {
		return ErrUpstream
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return ErrUnavailable
	default:
	}
	if _, err = c.input.Write(append(data, '\n')); err != nil {
		c.stop()
		return ErrUnavailable
	}
	return nil
}

func (c *client) call(ctx context.Context, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stopOnCancel := context.AfterFunc(ctx, c.stop)
	defer stopOnCancel()
	id := c.next.Add(1)
	key, _ := json.Marshal(id)
	answer := make(chan frame, 1)
	c.mu.Lock()
	if len(c.pending) >= 64 {
		c.mu.Unlock()
		return ErrBusy
	}
	c.pending[string(key)] = answer
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, string(key)); c.mu.Unlock() }()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case f := <-answer:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(f.Error) > 0 && string(f.Error) != "null" {
			return ErrUpstream
		}
		if result != nil && json.Unmarshal(f.Result, result) != nil {
			return ErrUnavailable
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrUnavailable
	}
}

func (c *client) stop() {
	c.once.Do(func() { close(c.done); _ = c.input.Close(); _ = c.output.Close(); _ = killProcessGroup(c.cmd) })
}
func (c *client) close() { c.stop(); <-c.exited }
