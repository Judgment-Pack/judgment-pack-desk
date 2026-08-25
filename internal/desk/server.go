// Package desk implements the jpack-desk chassis: an embedded single-page
// application plus one generic JSON-RPC relay to a `jpack mcp` subprocess.
//
// The chassis deliberately has no per-feature endpoints. Everything the desk
// can show, it asks the runtime for over the relay, so the desk's surface
// grows when the runtime's tool list grows and not otherwise.
package desk

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

// ShutdownGrace bounds how long an interrupted server waits for in-flight
// requests before it stops regardless.
const ShutdownGrace = 3 * time.Second

// devOrigins are additionally accepted when a --dev-token was supplied. The
// Vite dev server proxies /ws to this chassis, so the browser's Origin is the
// dev server's and never matches the Host it reaches us under.
var devOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

// Config is the chassis' whole configuration.
type Config struct {
	// ProjectDir is the absolute path of the Judgment Pack project. It becomes
	// the working directory of every `jpack mcp` subprocess, which is how the
	// runtime finds jpack.json, and it is the tree the file watcher watches.
	ProjectDir string
	// JpackBin names the runtime binary: a path, or a name resolved on PATH.
	JpackBin string
	// Token must be presented as ?token= on /ws.
	Token string
	// Static is the built SPA, rooted at its index.html.
	Static fs.FS
	// DevMode additionally accepts the Vite dev-server origin on /ws.
	DevMode bool
	// Logger receives subprocess stderr and relay diagnostics.
	Logger *log.Logger
}

// Server is the HTTP handler and the owner of the file watcher.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	static http.Handler
	log    *log.Logger

	mu    sync.Mutex
	conns map[*conn]struct{}

	watcher *watcher
}

// NewToken returns a fresh random session token.
func NewToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// New builds a server. The caller must Close it to release the file watcher.
func New(cfg Config) (*Server, error) {
	if cfg.ProjectDir == "" {
		return nil, errors.New("desk: ProjectDir is required")
	}
	if cfg.Token == "" {
		return nil, errors.New("desk: Token is required")
	}
	if cfg.JpackBin == "" {
		cfg.JpackBin = "jpack"
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	s := &Server{
		cfg:   cfg,
		mux:   http.NewServeMux(),
		log:   cfg.Logger,
		conns: make(map[*conn]struct{}),
	}
	if cfg.Static != nil {
		s.static = http.FileServer(http.FS(cfg.Static))
	}
	s.mux.HandleFunc("/ws", s.handleWS)
	// The file API (issue #14, phase 1). Everything else the desk shows comes
	// over the relay; writes cannot, because the runtime has no write tools by
	// design. See files.go for what this does and does not decide.
	s.mux.HandleFunc("GET /api/files", s.handleFiles)
	s.mux.HandleFunc("GET /api/file", s.handleFileRead)
	s.mux.HandleFunc("PUT /api/file", s.handleFileWrite)
	s.mux.HandleFunc("/", s.handleStatic)

	w, err := newWatcher(cfg.ProjectDir, s.log, s.broadcastFileChange)
	if err != nil {
		// A desk without live reload is still a working desk; a desk that
		// refuses to start because the tree is large or the inotify budget is
		// spent is not. Report and continue.
		s.log.Printf("desk: file watching disabled: %v", err)
	} else {
		s.watcher = w
	}
	return s, nil
}

// Close stops the file watcher. Open relays end with their sockets.
func (s *Server) Close() error {
	if s.watcher != nil {
		return s.watcher.Close()
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// authorized reports whether the request carries the session token. The
// comparison is constant-time so that a wrong token leaks no prefix.
func (s *Server) authorized(r *http.Request) bool {
	got := r.URL.Query().Get("token")
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) == 1
}

// originAllowed reports whether a WebSocket upgrade may proceed.
//
// A browser always sends Origin, so an Origin that is not the origin we were
// served under is a cross-site attempt and is refused: this is what stops a
// page on another site from driving the runtime through the visitor's own
// loopback. A request with no Origin at all is not from a browser — it is a
// script or a test holding the token — and the token is its authorization.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if s.cfg.DevMode {
		for _, allowed := range devOrigins {
			if strings.EqualFold(origin, allowed) {
				return true
			}
		}
	}
	return false
}

// handleStatic serves the embedded SPA with single-page fallback: a path that
// names no embedded file is a client-side route, so index.html answers it and
// the router in the page resolves it.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == nil {
		http.Error(w, "no embedded assets in this build", http.StatusNotFound)
		return
	}
	if _, err := fs.Stat(s.cfg.Static, "index.html"); err != nil {
		http.Error(w, "the single-page application has not been built: run `npm --prefix web ci && npm --prefix web run build`, then rebuild jpack-desk", http.StatusNotFound)
		return
	}
	clean := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." || clean == "/" {
		clean = "index.html"
	}
	if _, err := fs.Stat(s.cfg.Static, clean); err != nil {
		// Client-side route: hand back the shell. Never rewrite a request for a
		// missing asset, which should stay a 404 the build can be blamed for.
		if path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}
	s.static.ServeHTTP(w, r)
}

// handleWS is the whole relay surface: one WebSocket, one `jpack mcp`
// subprocess, JSON-RPC bytes passed through untouched in both directions.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "missing or invalid session token", http.StatusUnauthorized)
		return
	}
	if !s.originAllowed(r) {
		http.Error(w, fmt.Sprintf("origin %q is not permitted", r.Header.Get("Origin")), http.StatusForbidden)
		return
	}
	s.relay(w, r)
}
