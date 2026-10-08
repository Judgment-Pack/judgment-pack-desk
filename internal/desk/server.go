// Package desk implements the jpack-desk chassis: an embedded single-page
// application plus one generic JSON-RPC relay to a `jpack mcp` subprocess.
//
// The chassis deliberately has no per-feature endpoints. Everything the desk
// can show, it asks the runtime for over the relay, so the desk's surface
// grows when the runtime's tool list grows and not otherwise.
package desk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

	"github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan"
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
	// Managed desks share installation credentials and authentication, never data roots.
	parent *Server
	deskID string

	// RunnerBin is a trusted installed executable, never project configuration.
	RunnerBin string
	// RunnerInputProfiles contains installation-authorized public trust metadata.
	// It must never come from project configuration or a browser request.
	RunnerInputProfiles json.RawMessage
	RunnerConnections   json.RawMessage
	// RunnerAllowUntestedReleases turns off the installation's release policy.
	// The policy is on unless this is true: the Runner refuses a new job from
	// a release whose saved tests were not run (ADR-0009). The zero value is
	// the safe one, so a Config built without the startup flags, or a caller
	// that forgets them, keeps the policy on. Only the owner's
	// `--runner-require-tested-releases=false` sets it, never project
	// configuration or a browser request, and every desk of the installation
	// inherits it. `GET /api/desk-config` reports the policy to the page.
	RunnerAllowUntestedReleases bool
	// CodexBin is an advanced installation override: empty manages the runtime,
	// "off" disables it, otherwise an absolute trusted executable path.
	// It is never read from project configuration or browser requests.
	CodexBin string

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
	// LocalGatewayBundle is the trusted installation directory; empty disables management.
	LocalGatewayBundle string
	// Port is the TCP port the listener this server is served behind is bound
	// to, and it is **required**.
	//
	// It exists because the **launch handoff's** cookie name carries it. A
	// cookie's origin does not include the port — it never has — so a cookie
	// set for `127.0.0.1` is sent to every port on that host, and two desks on
	// one machine would share, and overwrite, one handoff. Naming it for the
	// port they were bound to keeps them apart. Nothing else on this desk is a
	// cookie: the session itself is a bearer the page holds, precisely because
	// naming a cookie for a port is a convention and not a boundary. See
	// session.go.
	//
	// It is required rather than defaulted because a default would be a port
	// some other desk is on: a zero here would name every desk's handoff
	// `jpack-desk-launch-0`, which is the shared-cookie flaw with a longer
	// name.
	Port int
	// Token is the **launch secret**: the one credential that is not minted
	// by this desk, and the only thing `GET /launch` trades for a handoff.
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
	// LocalAccess permits automatic loopback browser sessions before OIDC activation.
	LocalAccess bool
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
	desksMu     sync.Mutex
	desks       map[string]*Server
	desksClosed bool
	// resuming is set, under desksMu, while the start resumes the desks and
	// recovers their rotations; heldGates is each resumed desk's Runner
	// start gate, released once recovery has run (`resumeDesks`).
	resuming  bool
	heldGates []chan struct{}
	// deskCreations is how many desks are being made, which count toward
	// the registry's bound while the lock is released (`createDesk`).
	deskCreations int
	// reviewMu serializes this desk's lock confirmations (`handleReviewLock`)
	// and upgrades (`handleUpgradeConfirm`), from the fresh reading to the
	// last restore; this project's lock (project_lock.go) serializes them
	// with every other Desk process's.
	reviewMu sync.Mutex
	// startup is the identity of the project Desk was started on, which
	// names what Desk keeps for it (startup_identity.go); unused on a desk
	// Desk made.
	startup startupIdentity
	// keyMu serializes what this desk does with its signing key: the
	// decision record's reading of its keys, a rotation from the token to the
	// marker's removal, and the finish or undo of one a stop cut short
	// (rotation.go).
	keyMu sync.Mutex
	// handoverMu serializes every reading and change of this desk's record
	// of checkpoint hand-overs, `.desk-private/handover` (handover.go).
	handoverMu sync.Mutex
	// repairMu serializes this desk's repairs of its trail, from the fresh
	// reading the token is held to until the runtime's answer
	// (audit_repair.go), so that two confirmations in this process run one
	// repair.
	repairMu sync.Mutex
	// repairNonces holds the nonce of each repair token spent in this
	// process, under repairMu: a token confirms one attempt.
	repairNonces map[string]bool
	// stampingMu guards this desk's stamping settings in Desk's
	// configuration folder: read under it, written and removed under it for
	// writing, and held for reading while the decision record's runtime
	// reads the roots (stamping.go). stampingNonces holds the nonce of each
	// stamping token spent in this process, under it for writing.
	stampingMu     sync.RWMutex
	stampingNonces map[string]bool
	// stamping is this desk's scheduler of stamps: one loop, one run at a
	// time, stopped by Close (stamping.go).
	stamping *stampScheduler
	// reviewKey is this desk's own key for review tokens (`reviewToken`):
	// random per process, so a token names one desk and does not outlive it.
	reviewKey [32]byte

	updates    *updateService
	builds     ComponentBuilds
	jobs       *jobsCompanion
	codex      providerAccountManager
	aiAccounts aiManagers

	localGateway        *localGateway
	providerMu          sync.Mutex
	providerConnections map[string]*connectionCompanion
	providersClosed     bool
	connections         connectionCompanion
	gmailConnections    connectionCompanion
	notionConnections   connectionCompanion
	obsidianConnections connectionCompanion
	cfg                 Config
	mux                 *http.ServeMux
	static              http.Handler
	log                 *log.Logger

	mu        sync.Mutex
	conns     map[*conn]struct{}
	closing   bool
	relayWork sync.WaitGroup

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
	// researchSlots bounds the research relay the same way. Its own channel,
	// because a research run holding both of these must not starve the model
	// relay the same run is talking to. See researchrelay.go.
	researchSlots chan struct{}
	// writes serializes the compare-and-commit of every write. One mutex, not
	// one per path: a per-path key is a *spelling*, and two spellings of one
	// file on a case-insensitive filesystem would take different locks and both
	// commit. Desk-scale contention is not worth a correctness argument.
	writes *sync.Mutex
	// launchCookie is this desk's handoff cookie, port and all. Computed once,
	// here, so that the name a launch sets and the name the exchange reads
	// cannot drift apart. It is the **only** cookie this chassis has.
	launchCookie string
	// launches are the handoffs `GET /launch` has minted and `POST
	// /api/session` has not yet spent: single use, sixty seconds.
	launches *launchStore
	// sessions is the set of live sessions, minted by the exchange and
	// presented as a bearer id the page puts on each request itself. See
	// session.go for why nothing ambient authorizes anything.
	sessions *sessionStore
	signIn   *signInState
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
// requireTestedReleases is the installation's release policy, derived from
// the one field that can turn it off. The boot line and the desk-config
// answer both read it here.
func (cfg Config) requireTestedReleases() bool {
	return !cfg.RunnerAllowUntestedReleases
}

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
		return nil, errors.New("desk: Port is required: the launch handoff's cookie name carries it")
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

	if cfg.deskID == "" {
		data, err := readPrivateData(pinned.own.root, deskManifest, 4096)
		if err == nil {
			var record deskRecord
			if json.Unmarshal(data, &record) != nil || !deskIDPattern.MatchString(record.ID) || !validDeskName(record.Name) {
				return nil, errors.New("desk metadata is invalid")
			}
			cfg.deskID = record.ID
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
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
	writes := &sync.Mutex{}
	if cfg.parent != nil {
		writes = cfg.parent.writes
	}
	s := &Server{
		writes:        writes,
		builds:        componentBuilds(cfg.JpackBin, cfg.RunnerBin),
		cfg:           cfg,
		mux:           http.NewServeMux(),
		log:           cfg.Logger,
		conns:         make(map[*conn]struct{}),
		root:          pinned.own.root,
		project:       pinned,
		projectDir:    pinned.dir,
		configDir:     configDirFor(cfg.DeskConfigDir),
		relaySlots:    make(chan struct{}, maxRelayInFlight),
		researchSlots: make(chan struct{}, maxResearchInFlight),
		sessions:      sessions,
		launches:      launches,
		launchCookie:  launchCookieName(cfg.Port),
	}
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
	// crypto/rand never fails, and never returns short (Go 1.24 and later).
	_, _ = rand.Read(s.reviewKey[:])
	// The startup project keeps an inherited JPACK_CONFIG (`runtimeEnv`).
	// Said once, because Desk's editor shows this project's own jpack.json.
	if path := strings.TrimSpace(os.Getenv(runtimeConfigEnv)); path != "" && cfg.deskID == "" {
		s.log.Printf("desk: JPACK_CONFIG is set, so this project's runtime reads %s, not the project's own jpack.json; desks Desk made ignore it", path)
	}
	// And an inherited JPACK_SIGNING_KEY, said once beside it. Never its path,
	// which names where a secret is kept, and never its bytes, which this desk
	// does not read.
	if s.inheritsSigningKey() {
		s.log.Print("desk: JPACK_SIGNING_KEY is set, so where this project's audit trail is chained, its runtime signs each record with the key it names, if it accepts that key; desks Desk made ignore it")
	}
	if cfg.parent != nil {
		s.sessions = cfg.parent.sessions
		s.signIn = cfg.parent.signIn
	} else {
		s.openSignIn()
	}
	s.registerSignIn()
	if cfg.LocalGatewayBundle != "" {
		executable, _ := os.Executable()
		s.localGateway = &localGateway{bundle: cfg.LocalGatewayBundle, executable: executable}
	}
	s.removeStaleStaging()
	if cfg.Static != nil {
		s.static = http.FileServer(http.FS(cfg.Static))
	}
	// The launch exchange: the one path the launch secret is ever sent to, and
	// the only route on this chassis that is not gated — it is where
	// authorization is acquired rather than spent. See `handleLaunch`.
	// Registered without a method on purpose: `GET /launch` would match `HEAD`
	// too, and a `HEAD` that minted a handoff is a credential handed to a
	// request that carries no body. `handleLaunch` answers 405 to everything
	// but `GET`.
	s.mux.HandleFunc("/launch", s.handleLaunch)
	// And nothing under it: a near miss must not fall through to the SPA
	// fallback carrying the secret it was sent with.
	s.mux.HandleFunc("/launch/{rest...}", s.handleLaunchSubpath)
	s.mux.HandleFunc("/ws", s.handleWS)
	// The session: begun here and described here, and ended nowhere.
	//
	// `POST` is **the exchange** — the one route a cookie opens, and it spends
	// the cookie doing it. `GET` reports the bearer's record, which is what an
	// identity provider fills in. There is no `DELETE`: see `handleSession`.
	s.mux.HandleFunc("/api/session", s.handleSession)
	s.mux.HandleFunc("/api/operations/{rest...}", s.handleJobs)
	s.mux.HandleFunc("/api/job-events/{trigger}", s.handleJobEvent)
	s.mux.HandleFunc("/api/job-events/{trigger}/occurrences/{occurrence}", s.handleJobEventResult)
	s.mux.HandleFunc("/api/agent/run", s.handleAgentRun)
	s.mux.HandleFunc("/api/ai-connections", s.handleAIConnections)
	s.mux.HandleFunc("/api/model-providers", s.handleModelProviders)
	s.mux.HandleFunc("/api/model-providers/openai/{action}", s.handleModelProviders)
	// The file API (issue #14, phase 1). Everything else the desk shows comes
	// over the relay; writes cannot, because the runtime has no write tools by
	// design. See files.go for what this does and does not decide.
	s.mux.HandleFunc("/api/desks", s.handleDesks)
	s.mux.HandleFunc("/api/desks/{desk}/job-events/{trigger}", s.handleDeskJobEvent)
	s.mux.HandleFunc("/api/desks/{desk}/job-events/{trigger}/occurrences/{occurrence}", s.handleDeskJobEventResult)
	s.mux.HandleFunc("GET /api/files", s.handleFiles)
	s.mux.HandleFunc("GET /api/file", s.handleFileRead)
	s.mux.HandleFunc("PUT /api/file", s.handleFileWrite)
	// The assistant slot. Four endpoints, under the same guard, and each one
	// exists because the browser cannot do the thing itself: the desk-level
	// file is outside the project's pinned root, the key must never be pasted
	// into a project file, and the key must never reach the page. See
	// assistant.go for the whole argument.
	if s.cfg.parent != nil {
		s.updates = s.cfg.parent.updates
	} else {
		s.updates = newUpdateService(s.cfg.DevMode || s.builds.Desk.Modified || releaseplan.Version == "development")
		s.updates.start()
	}
	s.mux.HandleFunc("GET /api/updates", s.handleUpdates)
	s.mux.HandleFunc("POST /api/updates", s.handleUpdates)

	s.mux.HandleFunc("GET /api/desk-config", s.handleDeskConfig)
	s.mux.HandleFunc("GET /api/runner-key", s.handleRunnerKey)
	s.mux.HandleFunc("POST /api/document-processing/{method}", s.handleDocumentProcessing)
	s.mux.HandleFunc("POST /api/connections/{method}", s.handleConnections)
	s.mux.HandleFunc("POST /api/connections/{provider}/{method}", s.handleConnections)
	s.mux.HandleFunc("GET /api/attachments/{id}", s.handleAttachment)
	s.mux.HandleFunc("PUT /api/attachments/{id}", s.handleAttachment)
	s.mux.HandleFunc("GET /api/review", s.handleReview)
	s.mux.HandleFunc("POST /api/review/lock", s.handleReviewLock)
	s.mux.HandleFunc("GET /api/audit/verify", s.handleAuditVerify)
	s.mux.HandleFunc("GET /api/audit/trail", s.handleAuditTrail)
	s.mux.HandleFunc("POST /api/audit/key/rotate", s.handleRotateKey)
	s.mux.HandleFunc("POST /api/project/identity", s.handleResolveIdentity)
	s.mux.HandleFunc("POST /api/audit/repair", s.handleAuditRepair)
	s.mux.HandleFunc("GET /api/audit/holders", s.handleHolders)
	s.mux.HandleFunc("POST /api/audit/holders", s.handleAddHolder)
	s.mux.HandleFunc("POST /api/audit/holders/{id}/confirm", s.handleConfirmHandover)
	s.mux.HandleFunc("GET /api/audit/checkpoints", s.handleCheckpoints)
	s.mux.HandleFunc("GET /api/audit/jobs-verify", s.handleJobsVerify)
	s.mux.HandleFunc("POST /api/audit/stamping/check", s.handleStampingCheck)
	s.mux.HandleFunc("POST /api/audit/stamping", s.handleStampingSet)
	s.mux.HandleFunc("POST /api/audit/stamping/remove", s.handleStampingRemove)
	s.mux.HandleFunc("POST /api/audit/stamping/stamp", s.handleStampNow)
	s.mux.HandleFunc("GET /api/upgrade", s.handleUpgrade)
	s.mux.HandleFunc("POST /api/upgrade", s.handleUpgradeConfirm)
	s.mux.HandleFunc("GET /api/source-reviews", s.handleSourceReviews)
	s.mux.HandleFunc("PUT /api/source-reviews", s.handleSourceReviews)
	s.mux.HandleFunc("GET /api/briefs", s.handlePackTests)
	s.mux.HandleFunc("POST /api/briefs", s.handlePackTests)
	s.mux.HandleFunc("GET /api/pack-tests", s.handlePackTests)
	s.mux.HandleFunc("PUT /api/pack-tests", s.handlePackTests)
	s.mux.HandleFunc("GET /api/draft-packs", s.handleDraftPacks)
	s.mux.HandleFunc("PUT /api/draft-packs", s.handleDraftPacks)
	s.mux.HandleFunc("GET /api/conversations", s.handleConversations)
	s.mux.HandleFunc("PUT /api/conversations", s.handleConversations)
	s.mux.HandleFunc("GET /api/storage", s.handleStorage)
	s.mux.HandleFunc("POST /api/storage/move", s.handleStorageMove)
	s.mux.HandleFunc("POST /api/storage/project-history/preview", s.handleProjectHistory)
	s.mux.HandleFunc("POST /api/storage/project-history/relink", s.handleProjectHistory)
	s.mux.HandleFunc("GET /api/storage/backup", s.handleStorageBackup)
	s.mux.HandleFunc("POST /api/storage/restore", s.handleStorageRestore)
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
	// The research relay: the second such route, carrying the page's calls to
	// the configured judgment-pack gateway — acquire, seal, registry, by name
	// and nothing else — with no credential in either direction. See
	// researchrelay.go for the whole argument.
	s.mux.HandleFunc(researchPrefix+"{suffix...}", s.handleResearchRelay)
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
	// The identity of the project Desk was started on, before anything is
	// kept, stamped or recovered under its name (startup_identity.go).
	s.resolveStartupIdentity()
	s.initJobs()
	s.initModelProviders()
	// This desk's stamping, off the decision path: a loop that stamps where
	// the owner set an authority, until Close (stamping.go).
	s.startStamping()
	if cfg.parent == nil {
		s.resumeDesks()
	}
	// Runner's first start only now, after the start's sweep of the desks'
	// keys (jobs.go, `started`); and a desk the start resumes, only once the
	// start's recovery of rotations has run (issue #286), which releases it
	// (`resumeDesks`): its Runner's key decision would otherwise hold the
	// signing folder's lock that recovery takes.
	if s.jobs != nil {
		if parent := cfg.parent; parent != nil && parent.resuming {
			parent.heldGates = append(parent.heldGates, s.jobs.started)
		} else {
			close(s.jobs.started)
		}
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
	s.closeDesks()
	// Stop admission before waiting: upgraded sockets are not tracked by
	// http.Server.Shutdown, and a relay may still be starting its subprocess.
	s.mu.Lock()
	s.closing = true
	for c := range s.conns {
		c.cancel()
	}
	s.mu.Unlock()
	if s.jobs != nil {
		s.jobs.close()
	}
	// Before the project's folder is let go: a stamp in progress is waited
	// for, never killed mid-write.
	s.stamping.close()
	if s.cfg.parent == nil {
		s.closeAIAccounts()
	}
	if s.cfg.parent == nil && s.codex != nil {
		s.codex.Close()
	}
	if s.cfg.parent == nil {
		s.sessions.close()
		if s.updates != nil && s.updates.cancel != nil {
			s.updates.cancel()
		}
	}
	if s.cfg.parent == nil && s.signIn != nil {
		s.signIn.mu.Lock()
		s.signIn.cancelEpoch()
		s.signIn.mu.Unlock()
	}
	s.relayWork.Wait()
	s.providerMu.Lock()
	s.providersClosed = true
	companions := s.providerConnections
	s.providerMu.Unlock()
	for _, companion := range companions {
		companion.close()
	}
	s.connections.close()
	s.gmailConnections.close()
	s.notionConnections.close()
	s.obsidianConnections.close()
	if s.localGateway != nil {
		s.localGateway.close()
	}
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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r, finishPolicy := s.withSignInPolicy(r)
	defer finishPolicy()
	// Bind authenticated work to its session so revocation ends active relay
	// streams and runtime sockets, including a socket racing with sign-out.
	// Authorization remains in each existing guard; this does not grant access.
	id := bearerOf(r)
	if r.URL.Path == "/ws" {
		id, _ = offeredSessionID(r)
	}
	if held, ok := s.sessions.lookup(id); ok && !(r.URL.Path == "/api/session" && r.Method == http.MethodDelete) && r.URL.Path != "/api/auth/enable" {
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(held.ctx, cancel)
		defer stop()
		defer cancel()
		if held.ctx.Err() != nil {
			cancel()
		}
		r = r.WithContext(ctx)
	}
	if s.routeDesk(w, r) {
		return
	}
	s.mux.ServeHTTP(w, r)
}

// authorized reports whether the request may reach a gated capability, and
// there are exactly two ways to be — in this order.
//
//  1. **A session id the page put on the request itself**, as
//     `Authorization: Bearer <id>`. On a WebSocket upgrade the id arrives in
//     the `jpack-desk-session.<id>` subprotocol offer instead, which is the
//     only place a browser lets a page put anything on a handshake, and that
//     route reads its own offer — see `upgradeAuthorized`.
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
// **The launch secret is compared in constant time**, so a wrong one leaks no
// prefix. A session id is not compared at all: it is looked up by its MAC under
// a per-process key — a *keyed* lookup, not a constant-time comparison — which
// removes the signal rather than timing it away, because no id a caller can
// choose puts them nearer a live one. See `sessionStore`.
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
		refuseText(w, http.StatusNotFound, CodeNotFound, "no embedded assets in this build")
		return
	}
	if _, err := fs.Stat(s.cfg.Static, "index.html"); err != nil {
		refuseText(w, http.StatusNotFound, CodeNotFound,
			"the single-page application has not been built: run `npm --prefix web ci && npm --prefix web run build`, then rebuild jpack-desk")
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
			// **Marked, like every other refusal this chassis authors.** It is
			// a missing asset rather than a security answer, and marking it
			// anyway is what keeps "every refusal we wrote carries the mark" a
			// rule with no exceptions to remember.
			refuseText(w, http.StatusNotFound, CodeNotFound, "404 page not found")
			return
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}
	// **Wrapped for the same reason the upgrade is.** `http.FileServer` writes
	// its own `416` for a byte range nothing satisfies, and its own `404` and
	// `304`; the refusals among those are this chassis' answers and carry the
	// mark like every other.
	s.static.ServeHTTP(&marking{ResponseWriter: w, code: CodeBadRequest}, r)
}

// handleWS is the whole relay surface: one WebSocket, one `jpack mcp`
// subprocess, JSON-RPC bytes passed through untouched in both directions.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// The same two checks, in the same order, as every other gated route: the
	// session first, then the origin. The page's upgrade carries its session id
	// in the subprotocol offer, which is the only place a browser lets a page
	// put anything on a handshake; a script's carries the Bearer header.
	//
	// **A malformed offer is a 400 and not a 401.** "Your credential is wrong"
	// and "this handshake is not one this desk reads" are different answers, and
	// a caller that could not tell them apart would be told to fetch a new
	// session over a duplicate subprotocol.
	if _, problem := offeredSessionID(r); problem != "" {
		refuseText(w, http.StatusBadRequest, CodeBadRequest, problem)
		return
	}
	if !s.upgradeAuthorized(r) {
		refuseText(w, http.StatusUnauthorized, CodeUnauthorized,
			"no session: open the URL jpack-desk printed at startup")
		return
	}
	if !s.originAllowed(r) {
		refuseText(w, http.StatusForbidden, CodeForbidden,
			fmt.Sprintf("origin %q is not permitted", r.Header.Get("Origin")))
		return
	}
	// **Wrapped, because the library writes its own refusals.** A handshake
	// this desk authorized and `websocket.Accept` then refused — a missing key,
	// a version it does not speak — is a `400` no `writeJSON` ever sees, and it
	// is still a refusal this chassis authored.
	s.relay(&marking{ResponseWriter: w, code: CodeBadRequest}, r)
}

// upgradeAuthorized is `/ws`'s own gate, and it is narrower than the shared one
// on purpose.
//
// **On this route a session id is accepted only through the subprotocol
// offer.** The shared gate takes one on `Authorization` too, and admitting both
// here would be two ways to open the same socket with the same credential —
// two paths to keep in step, and one of them a path the browser cannot even
// use, since a `WebSocket` constructor has no header parameter. So this route
// takes one credential each way: the offer for a page, the launch secret for a
// script.
func (s *Server) upgradeAuthorized(r *http.Request) bool {
	if id, _ := offeredSessionID(r); id != "" {
		held, live := s.sessions.lookup(id)
		return live && s.signInSessionAllowed(held)
	}
	// No offer. A script's socket, and only the launch secret opens one: a
	// session id on this header authorizes nothing here.
	return s.launchSecretPresented(r)
}
