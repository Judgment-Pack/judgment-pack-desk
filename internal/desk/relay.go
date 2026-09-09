package desk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

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
	// handle is the MAC of the session this socket was opened with, or "" for
	// one opened with the launch secret.
	//
	// **A socket outlives the request that opened it**, which is the whole
	// point of a relay — so a session that is signed out or evicted while a
	// socket is open would otherwise leave that socket driving the runtime for
	// as long as it stayed connected. Recording the handle is what lets
	// `closeSession` find it. The handle rather than the id, so a live
	// credential is not sitting in a struct the whole process can reach.
	handle string
	// cancel stops this connection's context, which is how a sign-out reaches
	// the goroutines that own the socket.
	cancel context.CancelFunc
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

// closeSession ends every socket a session opened.
//
// Called by sign-out, and by the store when a session is evicted: in both cases
// the id names nothing afterwards, and a socket that kept driving the runtime
// would be a session that ended everywhere except where it mattered.
//
// The close carries a reason, so the page sees why rather than reconnecting
// into a refusal it cannot explain.
func (s *Server) closeSession(handle string) {
	if handle == "" {
		return
	}
	s.mu.Lock()
	ending := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		if c.handle == handle {
			ending = append(ending, c)
		}
	}
	s.mu.Unlock()
	for _, c := range ending {
		s.closeWith(c.ws, websocket.StatusPolicyViolation, "this session has ended")
		if c.cancel != nil {
			c.cancel()
		}
		c.stop()
	}
}

// touchedSession is how often an open socket refreshes its session's recency.
//
// **Once a second at most.** A busy relay sends many frames and each one taking
// the store's mutex would be a lock convoy for a fact that changes slowly; the
// property being kept is "a tab somebody is using is not the coldest", and a
// second's resolution says that perfectly well.
const touchedSession = time.Second

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
	// **`jpack-desk` is selected, and the session offer never is.** The page
	// offers two subprotocols — `jpack-desk`, and `jpack-desk-session.<id>`
	// carrying the credential — because a browser's `WebSocket` constructor has
	// no header parameter and the id must not go on the URL. Naming only the
	// plain one here means the id is read off the offer and **not echoed** in
	// the response, so it never appears in a header a proxy or a log would keep.
	//
	// `InsecureSkipVerify` stays: this desk does its own Origin check, in
	// `handleWS`, which is stricter than the library's and knows about
	// `--dev-token`.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		Subprotocols:       []string{wsProtocol},
	})
	if err != nil {
		s.log.Printf("desk: websocket upgrade failed: %v", err)
		return
	}
	ws.SetReadLimit(readLimit)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// The session this socket belongs to, recorded now: it is what sign-out and
	// eviction find, and what each inbound frame refreshes.
	offered, _ := offeredSessionID(r)
	handle := ""
	if offered != "" {
		handle = s.sessions.handle(offered)
	}
	c := &conn{
		ws:     ws,
		out:    make(chan []byte, outBuffer),
		done:   make(chan struct{}),
		handle: handle,
		cancel: cancel,
	}
	s.register(c)
	defer s.unregister(c)
	defer c.stop()

	cmd, err := s.runtimeCommand(ctx)
	if err != nil {
		s.log.Printf("desk: no runtime was started: %v", err)
		s.closeWith(ws, websocket.StatusInternalError, err.Error())
		return
	}
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
	// **Immediately before the spawn, and nothing between.** Where the host
	// cannot name a descriptor this is a check-then-use, so every statement
	// between the check and the `chdir` is window: command construction and
	// three pipe setups used to sit in it. See `aimAtTheProject`.
	if err := s.aimAtTheProject(cmd); err != nil {
		s.log.Printf("desk: no runtime was started: %v", err)
		s.closeWith(ws, websocket.StatusInternalError, err.Error())
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
	lastTouched := time.Time{}
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
			// **Traffic is use.** A tab that has been driving the runtime for an
			// hour has not been "looked up" once since its bootstrap, so under a
			// least-recently-used bound it was the coldest thing in the store
			// and the first to be evicted — the busiest desk being the one that
			// stopped working. Each frame refreshes it, at most once a second.
			if handle != "" && time.Since(lastTouched) >= touchedSession {
				lastTouched = time.Now()
				s.sessions.touch(handle)
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

// runtimeProject is the identity of the directory a runtime this desk starts
// would be in.
//
// **Derived from the command that is actually built**, so it cannot drift from
// it: on Linux that is the descriptor the trampoline changes into, and
// elsewhere it is the pathname `aimAtTheProject` checked and set. A method that
// answered from a field instead would be a second account of the same fact, and
// the first thing to go stale.
func (s *Server) runtimeProject(cmd *exec.Cmd) (os.FileInfo, error) {
	if len(cmd.ExtraFiles) > 0 {
		return cmd.ExtraFiles[0].Stat()
	}
	if cmd.Dir == "" {
		return nil, errors.New("this command was not aimed at a project")
	}
	return os.Stat(cmd.Dir)
}

// `aimAtTheProject` is per platform: on Linux the command already carries the
// descriptor and there is nothing left to do, and elsewhere it is the identity
// check, called with nothing between it and `Start`. See `project_linux.go`
// and `project_other.go`.

// runtimeWorkingDirByPathname is the fallback, as its own function.
//
// One spelling of the check, tested directly on every platform: a rule written
// inline in the one branch that uses it would be unreachable from a Linux test
// run and so held by nothing at all.
func runtimeWorkingDirByPathname(dir string, pinned fs.FileInfo) (string, error) {
	if !sameDirectory(dir, pinned) {
		return "", fmt.Errorf(
			"%s is no longer the project this desk pinned, so no runtime was started there", dir)
	}
	return dir, nil
}
