package desk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"sync"

	"github.com/coder/websocket"
)

const (
	// readLimit bounds one inbound WebSocket message. Requests are small; the
	// limit exists so a runaway page cannot grow the chassis' heap. The
	// library's 32 KiB default is far too small for the other direction, and
	// this is that ceiling raised deliberately rather than by accident.
	readLimit = 32 << 20
	// outBuffer is how many messages may queue for one socket before the
	// chassis stops trying. A client that has stopped reading is a client the
	// relay is entitled to give up on.
	outBuffer = 64
)

// conn is one browser connection. Every write to the socket goes through out,
// so the relay's stdout pump and the file watcher's broadcast never interleave
// two frames.
type conn struct {
	ws   *websocket.Conn
	out  chan []byte
	once sync.Once
	done chan struct{}
}

func (c *conn) send(msg []byte) {
	select {
	case c.out <- msg:
	case <-c.done:
	default:
		// Backed up: drop the connection rather than block the watcher or the
		// subprocess pump behind an unresponsive page.
		c.stop()
	}
}

func (c *conn) stop() { c.once.Do(func() { close(c.done) }) }

func (s *Server) register(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[c] = struct{}{}
}

func (s *Server) unregister(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}

// broadcastFileChange sends the one message this chassis originates: a
// JSON-RPC notification telling every open page that a file under the project
// changed, so it can invalidate what it cached. Everything else on the socket
// came from, or is going to, the runtime.
func (s *Server) broadcastFileChange(relPath string) {
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "desk/fileChanged",
		"params":  map[string]any{"path": relPath},
	})
	if err != nil {
		return
	}
	s.mu.Lock()
	targets := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		targets = append(targets, c)
	}
	s.mu.Unlock()
	for _, c := range targets {
		c.send(msg)
	}
}

// relay wires one WebSocket to one `jpack mcp` subprocess for the life of the
// socket. JSON-RPC bytes cross untouched: one message per text frame on the
// browser side, newline-delimited JSON on the stdio side. The chassis parses
// nothing and rewrites nothing, which is what keeps it generic — a tool added
// to the runtime tomorrow reaches the page with no change here.
func (s *Server) relay(w http.ResponseWriter, r *http.Request) {
	// Origin was already checked against the served origin (and, in dev mode,
	// the Vite origin) in handleWS; the library's own check would reject the
	// dev proxy and cannot see that decision.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.log.Printf("desk: websocket upgrade failed: %v", err)
		return
	}
	ws.SetReadLimit(readLimit)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := &conn{ws: ws, out: make(chan []byte, outBuffer), done: make(chan struct{})}
	s.register(c)
	defer s.unregister(c)
	defer c.stop()

	cmd := exec.CommandContext(ctx, s.cfg.JpackBin, "mcp")
	// The resolved project directory, not the configured pathname: the file API
	// holds a descriptor to this same directory, and a runtime started from a
	// pathname that has since been repointed would be judging a different tree
	// from the one the desk is writing.
	cmd.Dir = s.projectDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.closeWith(ws, websocket.StatusInternalError, "cannot open runtime stdin")
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.closeWith(ws, websocket.StatusInternalError, "cannot open runtime stdout")
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.closeWith(ws, websocket.StatusInternalError, "cannot open runtime stderr")
		return
	}
	if err := cmd.Start(); err != nil {
		s.log.Printf("desk: cannot start %q: %v", s.cfg.JpackBin, err)
		s.closeWith(ws, websocket.StatusInternalError, "cannot start the judgment-pack runtime: "+err.Error())
		return
	}

	var wg sync.WaitGroup
	wg.Add(4)

	// Runtime stderr is the runtime's own diagnostics; it is never protocol.
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			s.log.Printf("jpack mcp: %s", sc.Text())
		}
	}()

	// Socket writer: the only goroutine that touches ws for writing.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case msg := <-c.out:
				if err := ws.Write(ctx, websocket.MessageText, msg); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Runtime stdout -> socket, one newline-delimited JSON message per frame.
	// bufio.Reader rather than Scanner: a pack document can exceed any token
	// cap worth choosing, and Scanner would truncate the stream silently.
	go func() {
		defer wg.Done()
		defer cancel()
		br := bufio.NewReader(stdout)
		for {
			line, err := br.ReadBytes('\n')
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
				c.send(trimmed)
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					s.log.Printf("desk: reading runtime stdout: %v", err)
				}
				return
			}
		}
	}()

	// Socket -> runtime stdin.
	go func() {
		defer wg.Done()
		defer cancel()
		defer stdin.Close()
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			// One JSON-RPC message per frame becomes one line. A frame that
			// carried an embedded newline would desynchronize the stdio side,
			// so it is refused rather than forwarded.
			if bytes.ContainsAny(data, "\r\n") {
				s.log.Printf("desk: refusing a frame containing a newline (%d bytes)", len(data))
				continue
			}
			if _, err := stdin.Write(append(data, '\n')); err != nil {
				return
			}
		}
	}()

	<-ctx.Done()
	c.stop()
	_ = stdin.Close()
	// CommandContext kills the child when ctx is cancelled; Wait reaps it so
	// no `jpack mcp` outlives the socket that opened it.
	_ = cmd.Wait()
	_ = ws.CloseNow()
	wg.Wait()
}

func (s *Server) closeWith(ws *websocket.Conn, code websocket.StatusCode, reason string) {
	_ = ws.Close(code, reason)
}
