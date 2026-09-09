// Package desk implements the jpack-desk chassis: an embedded single-page
// application plus one generic JSON-RPC relay to a `jpack mcp` subprocess.
//
// The chassis deliberately has no per-feature endpoints. Everything the desk
// can show, it asks the runtime for over the relay, so the desk's surface
// grows when the runtime's tool list grows and not otherwise.
package desk

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// ShutdownGrace bounds how long an interrupted server waits for in-flight
// requests before it stops regardless.
const ShutdownGrace = 3 * time.Second

// devOrigins are additionally accepted when a --dev-token was supplied. The
// Vite dev server proxies /launch, /ws and /api to this chassis, so the
// browser's Origin is the dev server's and never matches the Host it reaches us
// under.
var devOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

// Config is the chassis' whole configuration.
type Config struct {
	// ProjectDir is the path of the Judgment Pack project. It becomes the
	// working directory of every `jpack mcp` subprocess, which is how the
	// runtime finds jpack.json, and it is the tree the file watcher watches.
	//
	// Ignored where Root is given, and required where it is not.
	ProjectDir string
	// Root is a project directory a caller has **already validated and
	// pinned**, and is how a launch hands one over.
	//
	// **The reason it exists is that a pathname cannot be handed over
	// safely.** The launch validates a configured `jpack-desk.json` and the
	// directory it is in; if it passed a *name* here, this constructor would
	// resolve that name again, and a rename between the two would let another
	// tree be pinned than the one that was validated. So what crosses the
	// boundary is the descriptor. Where it is nil this constructor opens one
	// itself, through the same function and with the same identity check —
	// see `OpenProjectRoot`.
	//
	// **This server takes ownership of it**, on success and on failure alike:
	// a caller that also closed it would double-close, and one that closed
	// nothing on an error would leak a descriptor at startup.
	Root *ProjectRoot
	// JpackBin names the runtime binary: a path, or a name resolved on PATH.
	JpackBin string
	// Port is the TCP port the listener this server is served behind is bound
	// to, and it is **required**.
	//
	// It exists because the session cookie's name carries it. A cookie's origin
	// does not include the port — it never has — so a cookie set for
	// `127.0.0.1` is sent to every port on that host, and two desks on one
	// machine would share, and overwrite, one session. Naming the cookie for
	// the port they were bound to keeps them apart. See session.go.
	//
	// It is required rather than defaulted because a default would be a port
	// some other desk is on: a zero here would name every desk's cookie
	// `jpack-desk-session-0`, which is the shared-cookie flaw with a longer
	// name.
	Port int
	// Token is the **launch secret**: the one credential that is not minted
	// by this desk, and the only thing `GET /launch` trades for a session.
	//
	// It is presented exactly twice — once on the launch query by whoever
	// opens the printed URL, and as `Authorization: Bearer <secret>` by a
	// script that has no cookie jar. It is never on a request query otherwise,
	// and the page never holds it at all. See session.go.
	Token string
	// Static is the built SPA, rooted at its index.html.
	Static fs.FS
	// DevMode additionally accepts the Vite dev-server origin on /ws.
	DevMode bool
	// DeskConfigDir overrides where this machine's own desk-level directory
	// is — the desk.json this desk reads and the key it keeps. Empty is the
	// answer every running desk uses: ~/.config/jpack-desk, with
	// XDG_CONFIG_HOME honoured. It exists so a test can be hermetic against
	// the machine it runs on, and there is no flag that sets it.
	DeskConfigDir string
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

	// root is the project directory, pinned once. Every file-API operation goes
	// through it, so containment is a held directory descriptor rather than a
	// pathname that was true when it was checked — see files.go.
	root *os.Root
	// project is the whole pinned root, and this server owns it.
	//
	// It is kept beside `root` because the two consumers that cannot go
	// through `os.Root` — a subprocess's working directory and the file
	// watcher — need the descriptor itself. See `ProjectRoot` and
	// `runtimeWorkingDir`.
	project *ProjectRoot
	// projectDir is ProjectDir with its symlinks resolved, taken once at
	// construction. It is the pathname every part of the chassis that cannot
	// hold a descriptor uses: the runtime's working directory and the file
	// watcher. Resolving it once is what stops the two halves of the desk
	// operating on two different projects — repointing a symlinked ProjectDir
	// after startup would otherwise leave the file API writing the original
	// tree while each new runtime judged the replacement.
	projectDir string
	// configDir is this machine's desk-level directory, resolved once. Empty
	// where there is no home directory to resolve it against, which each
	// endpoint that needs it reports as a sentence rather than a dead desk.
	configDir string
	// assistant is that directory validated and pinned — or the reason it was
	// refused. Built once, at startup, and closed with the server. A desk whose
	// configuration directory is not safe to keep a credential in still runs;
	// it simply will not keep one, and says which directory is the reason. See
	// custody.go.
	assistant *assistantStore
	// relaySlots bounds how many model-relay requests are in flight at once.
	// A buffered channel rather than a semaphore type: taking a slot without
	// waiting is one `select` with a `default`, which is exactly the "a bound,
	// not a queue" the relay promises. See modelrelay.go.
	relaySlots chan struct{}
	// writes serializes the compare-and-commit of every write. One mutex, not
	// one per path: a per-path key is a *spelling*, and two spellings of one
	// file on a case-insensitive filesystem would take different locks and both
	// commit. Desk-scale contention is not worth a correctness argument.
	writes sync.Mutex
	// launchCookie is this desk's handoff cookie, port and all. Computed once,
	// here, so that the name a launch sets and the name the exchange reads
	// cannot drift apart. It is the **only** cookie this chassis has.
	launchCookie string
	// pendingCookie is the readable marker set beside the handoff, so that a
	// page already holding a session id still knows to spend a new one. See
	// `pendingCookiePrefix`.
	pendingCookie string
	// launches are the handoffs `GET /launch` has minted and `POST
	// /api/session` has not yet spent: single use, sixty seconds.
	launches *launchStore
	// sessions is the set of live sessions, minted by the exchange and
	// presented as a bearer id the page puts on each request itself. See
	// session.go for why nothing ambient authorizes anything.
	sessions *sessionStore
	// closeOnce makes shutdown idempotent; see Close.
	closeOnce sync.Once
	closeErr  error
}

// NewToken returns a fresh 192-bit random secret, hex encoded.
//
// It mints two different things, and they are deliberately the same strength:
// the **launch secret** a desk prints at startup, and each **session id** the
// launch exchange trades it for. Both are bearer credentials for this desk's
// whole surface, so neither may be the weaker of the two.
func NewToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// New builds a server. The caller must Close it to release the file watcher.
func New(cfg Config) (*Server, error) {
	// **Adopted first, released on every failure.** This server owns the
	// descriptor it was handed, so a refusal below has to close it rather than
	// leave it open in a process that is about to exit or retry.
	pinned := cfg.Root
	adopted := false
	defer func() {
		if !adopted {
			pinned.Close()
		}
	}()
	if pinned == nil && cfg.ProjectDir == "" {
		return nil, errors.New("desk: ProjectDir is required")
	}
	if cfg.Token == "" {
		return nil, errors.New("desk: Token is required")
	}
	if cfg.Port <= 0 {
		return nil, errors.New("desk: Port is required: the session cookie's name carries it")
	}
	if cfg.JpackBin == "" {
		cfg.JpackBin = "jpack"
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	// **Validated and pinned in one operation, here or by the launch.** Two
	// pathname resolutions — one to check and one to open — leave a window in
	// which the directory checked is not the directory served; see
	// `OpenProjectRoot`, which is the only place either happens.
	if pinned == nil {
		var err error
		if pinned, err = OpenProjectRoot(cfg.ProjectDir); err != nil {
			return nil, fmt.Errorf("desk: project directory: %w", err)
		}
	}
	sessions, serr := newSessionStore()
	if serr != nil {
		return nil, fmt.Errorf("desk: session store: %w", serr)
	}
	launches, lerr := newLaunchStore()
	if lerr != nil {
		return nil, fmt.Errorf("desk: launch store: %w", lerr)
	}
	s := &Server{
		cfg:           cfg,
		mux:           http.NewServeMux(),
		log:           cfg.Logger,
		conns:         make(map[*conn]struct{}),
		root:          pinned.own.root,
		project:       pinned,
		projectDir:    pinned.dir,
		configDir:     configDirFor(cfg.DeskConfigDir),
		relaySlots:    make(chan struct{}, maxRelayInFlight),
		sessions:      sessions,
		launches:      launches,
		launchCookie:  launchCookieName(cfg.Port),
		pendingCookie: pendingCookieName(cfg.Port),
	}
	// A session that stops being one takes its sockets with it — sign-out, or
	// eviction under the bound. Registered here because the store is built
	// before the server it belongs to.
	sessions.whenEnded(s.closeSession)
	adopted = true
	// **One owner from here on.** The wrapper the caller still holds stops
	// owning anything, so a `Close` on it cannot take the descriptor out from
	// under a running server. See `ProjectRoot.detach`.
	pinned.detach()
	// Validated and pinned once. Doing it per request would let the authority
	// itself be retargeted between requests, which is the same argument the
	// project root is pinned for.
	s.assistant = openAssistantStore(s.configDir)
	if !s.assistant.usable() {
		// Reported, and not fatal. A desk that refused to start because
		// somebody's ~/.config is group-writable would be a desk nobody could
		// use to read a pack; what is withdrawn is the ability to keep a key.
		s.log.Printf("desk: no assistant key will be kept: %v", s.assistant.problem)
	}
	s.removeStaleStaging()
	if cfg.Static != nil {
		s.static = http.FileServer(http.FS(cfg.Static))
	}
	// The launch exchange: the one path the launch secret is sent to, and the
	// only route on this chassis that is not gated — it is where authorization
	// is acquired rather than spent. See `handleLaunch`.
	s.mux.HandleFunc("GET /launch", s.handleLaunch)
	// And nothing under it: a near miss must not fall through to the SPA
	// fallback carrying the secret it was sent with.
	s.mux.HandleFunc("/launch/{rest...}", s.handleLaunchSubpath)
	s.mux.HandleFunc("/ws", s.handleWS)
	// The session: begun here, described here, ended here.
	//
	// `POST` is **the exchange** — the one route a cookie opens, and it spends
	// the cookie doing it. `GET` reports the bearer's record, which is what an
	// identity provider fills in. `DELETE` is sign-out.
	s.mux.HandleFunc("/api/session", s.handleSession)
	// The file API (issue #14, phase 1). Everything else the desk shows comes
	// over the relay; writes cannot, because the runtime has no write tools by
	// design. See files.go for what this does and does not decide.
	s.mux.HandleFunc("GET /api/files", s.handleFiles)
	s.mux.HandleFunc("GET /api/file", s.handleFileRead)
	s.mux.HandleFunc("PUT /api/file", s.handleFileWrite)
	// The assistant slot. Four endpoints, under the same guard, and each one
	// exists because the browser cannot do the thing itself: the desk-level
	// file is outside the project's pinned root, the key must never be pasted
	// into a project file, and the key must never reach the page. See
	// assistant.go for the whole argument.
	s.mux.HandleFunc("GET /api/desk-config", s.handleDeskConfig)
	// The one write to that file, and it replaces one member of it. It is
	// under the same guard as everything else, takes no path, composes the
	// bytes itself and decodes them before any of them reach the disk. See
	// `handleDeskConfigWrite` for why a chassis with no per-feature endpoints
	// has this one.
	s.mux.HandleFunc("PUT /api/desk-config", s.handleDeskConfigWrite)
	s.mux.HandleFunc("GET /api/assistant/key", s.handleAssistantKeyRead)
	s.mux.HandleFunc("PUT /api/assistant/key", s.handleAssistantKeyWrite)
	s.mux.HandleFunc("DELETE /api/assistant/key", s.handleAssistantKeyDelete)
	s.mux.HandleFunc("POST /api/assistant/probe", s.handleAssistantProbe)
	// The model relay: the one route that carries traffic this chassis does
	// not read. It exists because the page runs the assistant's loop and the
	// key must never reach the page, so the request that presents it has to be
	// made here. Every method, because the protocols on the other side define
	// their own. See modelrelay.go for the whole argument.
	s.mux.HandleFunc(relayPrefix+"{suffix...}", s.handleModelRelay)
	s.mux.HandleFunc("/", s.handleStatic)

	// **Watched through the descriptor where the host has a way to name one.**
	// The watcher takes a path because inotify does; on Linux that path
	// resolves through this desk's own descriptor, so a rename of the project
	// cannot move what is being watched. Off Linux it is the resolved
	// spelling, as it always was.
	watchRoot := pinned.dir
	if through, ok := pinned.descriptorWorkingDir(); ok {
		watchRoot = through
	}
	w, werr := newWatcher(watchRoot, s.log, s.broadcastFileChange)
	if werr != nil {
		// A desk without live reload is still a working desk; a desk that
		// refuses to start because the tree is large or the inotify budget is
		// spent is not. Report and continue.
		s.log.Printf("desk: file watching disabled: %v", werr)
	} else {
		s.watcher = w
	}
	return s, nil
}

// Close stops the file watcher and releases the pinned project root. Open
// relays end with their sockets.
//
// **Once, however many times it is called.** Shutdown paths overlap — a
// deferred `Close` beside an explicit one, a test's cleanup beside its own —
// and closing a descriptor twice is closing whatever took its number in
// between.
func (s *Server) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.closeAll() })
	return s.closeErr
}

func (s *Server) closeAll() error {
	var err error
	if s.watcher != nil {
		err = s.watcher.Close()
	}
	if s.project != nil {
		// **Through the shared cell, which closes at most once**, and without
		// touching the adopted flag: a wrapper that was handed over stays
		// handed over, so a caller's deferred `Close` after this shutdown
		// still closes nothing.
		if rerr := s.project.own.close(); err == nil {
			err = rerr
		}
	}
	if s.assistant != nil {
		if aerr := s.assistant.Close(); err == nil {
			err = aerr
		}
	}
	return err
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// authorized reports whether the request may reach a gated capability, and
// there are exactly two ways to be — in this order.
//
//  1. **A session id the page put on the request itself** — as
//     `Authorization: Bearer <id>` on a `fetch`, or as the
//     `jpack-desk-session.<id>` subprotocol offer on a WebSocket upgrade, which
//     is the only place a browser lets a page put anything on one.
//  2. **`Authorization: Bearer <launch secret>`**, which is how a script, a
//     test or the containment gate authorizes — none of them has a cookie jar,
//     and all of them can read the secret the desk was started with.
//
// And **nothing else**. In particular **no cookie authorizes anything here**.
// The one cookie this chassis sets opens exactly one route, `POST
// /api/session`, and is spent by it. A credential the browser attaches by
// itself is a credential every other service on this host receives — cookies
// have no port — and one a script can replay; that is the class this shape
// removes rather than guards. The `?token=` query is gone for the same family
// of reason: a credential on a query is a credential in an address bar, a
// `Referer`, a proxy log and `Response.url`.
//
// Both comparisons are constant-time, so neither a wrong secret nor a wrong
// session id leaks a prefix. See session.go.
func (s *Server) authorized(r *http.Request) bool {
	if _, ok := s.sessionOf(r); ok {
		return true
	}
	return s.launchSecretPresented(r)
}

// originAllowed reports whether a request may proceed, and what it is worth
// depends on which route asked.
//
//   - On a **gated route** it is defence in depth. The session is a bearer this
//     page holds and puts on each request itself, so a cross-site page has
//     nothing to send and this guard refuses nothing it could otherwise do.
//   - On the **exchange** it is load-bearing, beside `Sec-Fetch-Site`: that is
//     the one route an ambient credential opens, and the only place a foreign
//     page could drive somebody's cookie.
//
// **What Origin is, and what it is not.** A browser sends it on every request
// that could change something — every `PUT`, every non-simple `fetch`, every
// WebSocket upgrade — and **not** on a same-origin `GET`, which carries no
// `Origin` at all. So this check refuses a cross-site attempt where there is
// one to refuse, and is silent about a plain read. That is the right division
// of labour here because a plain read is not authorized by anything ambient:
// since the session became a bearer the page puts on each request itself, a
// cross-site page cannot make an authorized request of any shape, and this
// guard is defence in depth over writes and upgrades rather than the thing
// standing between a foreign page and the runtime.
//
// **A request with no Origin at all is accepted here**, because a script
// legitimately sends none and a same-origin `GET` sends none either. That is
// not a hole, and the reason is that nothing ambient authorizes a request any
// more: whatever arrives without an `Origin` still has to carry a session id
// somebody deliberately put on it.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// A serialized origin is a scheme, a host and a port, and nothing else.
	// Matching on the host alone would accept `http://127.0.0.1:8791/evil` and
	// an origin whose scheme is not the one we were served under, while the
	// prose promises same-origin semantics.
	// "Nothing else" means nothing else, including the delimiters that parse to
	// empty: `http://host?` sets ForceQuery with an empty RawQuery, and a bare
	// trailing `#` is dropped by the parser entirely, so the raw header is
	// checked for it.
	if u.Scheme == "" || u.Host == "" || u.Opaque != "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		strings.ContainsRune(origin, '#') {
		return false
	}
	if !strings.EqualFold(u.Scheme, requestScheme(r)) {
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

// requestScheme is the scheme this request reached us under. The chassis binds
// loopback and serves plain HTTP; TLS is reported where a future build ends up
// terminating it here, and no forwarded header is trusted, because anything a
// proxy asserts is something an attacker can assert too.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// handleStatic serves the embedded SPA with single-page fallback: a path that
// names no embedded file is a client-side route, so index.html answers it and
// the router in the page resolves it.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// **Never the page, for anything that looks like a launch.** This handler
	// answers every path the router does not own, and it treats an unknown
	// extensionless path as a client-side route — so `/Launch/x?secret=…` was
	// answered *with the page*, leaving the secret in `window.location`, in
	// history and in every `Referer` that page went on to send. The router owns
	// `/launch` and `/launch/…` exactly; this owns the spellings it does not
	// see. See `looksLikeALaunch`.
	if looksLikeALaunch(r) {
		refuseLaunchShape(w)
		return
	}
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
	// The same two checks, in the same order, as every other gated route: the
	// session first, then the origin. The page's upgrade carries its session id
	// in the subprotocol offer, which is the only place a browser lets a page
	// put anything on a handshake; a script's carries the Bearer header.
	// **A malformed offer is a 400 and not a 401.** "Your credential is wrong"
	// and "this handshake is not one this desk reads" are different answers, and
	// a caller that could not tell them apart would be told to fetch a new
	// session over a duplicate subprotocol.
	if _, problem := offeredSessionID(r); problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "no session: open the URL jpack-desk printed at startup", http.StatusUnauthorized)
		return
	}
	if !s.originAllowed(r) {
		http.Error(w, fmt.Sprintf("origin %q is not permitted", r.Header.Get("Origin")), http.StatusForbidden)
		return
	}
	s.relay(w, r)
}
