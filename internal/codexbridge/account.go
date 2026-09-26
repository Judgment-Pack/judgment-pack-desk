package codexbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrAttempt   = errors.New("the Codex sign-in attempt is unavailable")
	ErrConnected = errors.New("disconnect the current ChatGPT account before signing in again")
)

type Options struct {
	// Empty Binary uses the Desk-managed runtime. An explicit override comes
	// from the installation owner, never project or HTTP input.
	Binary, ProfileDir, ProjectDir string
}

// Owner binds a transient challenge to a minted Desk session and its policy.
// Neither context may be the HTTP request context, which ends after delivery.
type Owner struct {
	ID              string
	Session, Policy context.Context
}

func (o Owner) alive() bool {
	return o.ID != "" && o.Session != nil && o.Policy != nil && o.Session.Err() == nil && o.Policy.Err() == nil
}

type LoginState struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Challenge struct {
	LoginChallenge
	ExpiresAt time.Time `json:"expiresAt"`
}

// Status reports account presence, not a promise of subscription entitlement.
// Native identifiers, email, upstream errors and credentials cannot appear here.
type Status struct {
	Provider     string      `json:"provider"`
	AuthMethod   string      `json:"authMethod"`
	Agent        string      `json:"agent"`
	Runtime      string      `json:"runtime"`
	Account      string      `json:"account"`
	Plan         string      `json:"plan,omitempty"`
	LastVerified *time.Time  `json:"lastVerified,omitempty"`
	Login        *LoginState `json:"login,omitempty"`
}

type loginAttempt struct {
	state    LoginState
	nativeID string
	owner    Owner
	stops    []func() bool
}

func (p *loginAttempt) stop() {
	for _, stop := range p.stops {
		stop()
	}
}

// Manager supervises the private native account process. It does not expose
// raw RPC, inference, a project cwd or a tool-execution capability.
type Manager struct {
	mu            sync.Mutex
	active        *activeRun
	disconnecting bool
	closeOnce     sync.Once
	life          context.Context
	cancel        context.CancelFunc
	profile       *profile
	binary        string
	managed       bool
	resolveBinary func(context.Context, bool) (string, error)
	client        *client
	pending       *loginAttempt
	last          *LoginState
	lastOwner     string
	closed        bool
	workers       sync.WaitGroup
	// Private test seams; callers cannot override the native command or expiry.
	launch   func(context.Context, *exec.Cmd) (*client, error)
	loginTTL time.Duration
}

func NewManager(options Options) (*Manager, error) {
	binary := options.Binary
	if binary == "" {
		if !ManagedRuntimeSupported() {
			return nil, ErrUnavailable
		}
	} else {
		if !filepath.IsAbs(binary) {
			return nil, ErrUnavailable
		}
		var err error
		binary, err = filepath.EvalSymlinks(binary)
		if err != nil {
			return nil, ErrUnavailable
		}
		info, err := os.Stat(binary)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return nil, ErrUnavailable
		}
	}
	p, err := openProfile(options.ProfileDir, options.ProjectDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{life: ctx, cancel: cancel, profile: p, binary: binary, managed: options.Binary == "", resolveBinary: p.managedBinary, launch: startClient, loginTTL: 10 * time.Minute}, nil
}

func (m *Manager) operation(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.life, cancel)
	if m.life.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func (m *Manager) spawn(ctx context.Context) error {
	if m.closed || m.life.Err() != nil {
		return ErrUnavailable
	}
	if m.client != nil {
		select {
		case <-m.client.done:
			m.client.close()
			m.client = nil
		default:
			return nil
		}
	}
	if err := m.profile.check(); err != nil {
		return err
	}
	if m.managed {
		binary, err := m.resolveBinary(ctx, false)
		if err != nil {
			return err
		}
		m.binary = binary
	}
	c, err := m.launch(ctx, m.profile.command(m.binary))
	if err != nil {
		return err
	}
	m.client = c
	m.workers.Add(1)
	go m.watch(c)
	return nil
}
func (m *Manager) stopClient() {
	if m.client != nil {
		m.client.close()
		m.client = nil
	}
}
func (m *Manager) finish(state string) {
	if m.pending == nil {
		return
	}
	p := m.pending
	p.stop()
	result := p.state
	result.State = state
	m.last = &result
	m.lastOwner = p.owner.ID
	m.pending = nil
}
func (m *Manager) readAccount(ctx context.Context, refresh bool) (AccountState, error) {
	var raw json.RawMessage
	if err := m.client.call(ctx, "account/read", map[string]bool{"refreshToken": refresh}, &raw); err != nil {
		return AccountState{}, err
	}
	return accountState(raw)
}

// reset stops the login writer BEFORE logging out in a fresh process. A stale
// completion from the old process therefore cannot recreate auth after logout.
// The durable bit survives process crashes and any failed cleanup.
func (m *Manager) reset(ctx context.Context, state string) error {
	if err := m.profile.markCleanup(); err != nil {
		m.stopClient()
		m.finish(state)
		return err
	}
	nativeID := ""
	if m.pending != nil {
		nativeID = m.pending.nativeID
	}
	m.finish(state)
	if m.client != nil && nativeID != "" {
		cancelCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = m.client.call(cancelCtx, "account/login/cancel", map[string]string{"loginId": nativeID}, nil)
		cancel()
	}
	m.stopClient()
	if err := m.spawn(ctx); err != nil {
		return err
	}
	defer m.stopClient()
	if err := m.client.call(ctx, "account/logout", nil, nil); err != nil {
		return err
	}
	account, err := m.readAccount(ctx, false)
	if err != nil {
		return err
	}
	if account.Authenticated {
		return ErrUpstream
	}
	// Reap all native writers before the durable cleanup bit is removed.
	m.stopClient()
	return m.profile.clearCleanup()
}
func (m *Manager) ready(ctx context.Context) error {
	if m.closed {
		return ErrUnavailable
	}
	if m.pending != nil && (!m.pending.owner.alive() || !time.Now().Before(m.pending.state.ExpiresAt)) {
		state := "canceled"
		if !time.Now().Before(m.pending.state.ExpiresAt) {
			state = "expired"
		}
		if err := m.reset(ctx, state); err != nil {
			return err
		}
	}

	if m.pending != nil && m.client != nil {
		select {
		case <-m.client.done:
			if err := m.reset(ctx, "failed"); err != nil {
				return err
			}
		default:
		}
	}
	cleanup, err := m.profile.needsCleanup()
	if err != nil {
		return err
	}
	if cleanup && m.pending == nil {
		if err = m.reset(ctx, "failed"); err != nil {
			return err
		}
	}
	return m.spawn(ctx)
}

func (m *Manager) Status(ctx context.Context, ownerID string, refresh bool) (Status, error) {
	status := Status{Provider: "openai", AuthMethod: "subscription", Agent: "codex", Runtime: "unavailable", Account: "unavailable"}
	if !m.mu.TryLock() {
		return status, ErrBusy
	}
	defer m.mu.Unlock()
	if m.active != nil || m.disconnecting {
		return status, ErrBusy
	}
	ctx, release := m.operation(ctx)
	defer release()
	if err := m.ready(ctx); err != nil {
		if errors.Is(err, ErrNotInstalled) {
			status.Runtime = "not-installed"
			return status, nil
		}
		return status, err
	}
	status.Runtime = "available"
	if m.last != nil && m.lastOwner == ownerID {
		copy := *m.last
		status.Login = &copy
	}
	if m.pending != nil {
		status.Account = "login-pending"
		if m.pending.owner.ID == ownerID {
			copy := m.pending.state
			status.Login = &copy
		}
		return status, nil
	}
	account, err := m.readAccount(ctx, refresh)
	if err != nil {
		return status, err
	}
	if err = m.profile.check(); err != nil {
		m.stopClient()
		return status, err
	}
	now := time.Now().UTC()
	status.LastVerified = &now
	status.Account = "signed-out"
	if account.Authenticated {
		status.Account = "connected"
		status.Plan = account.Plan
	}
	return status, nil
}

func (m *Manager) StartLogin(ctx context.Context, owner Owner, method string) (Challenge, error) {
	params, err := loginParams(method)
	if err != nil || !owner.alive() {
		return Challenge{}, ErrAttempt
	}
	if !m.mu.TryLock() {
		return Challenge{}, ErrBusy
	}
	defer m.mu.Unlock()
	if m.active != nil || m.disconnecting {
		return Challenge{}, ErrBusy
	}
	ctx, release := m.operation(ctx)
	defer release()
	// Bind preparation to the requesting session before any download starts.
	prepareCtx, prepareCancel := context.WithCancel(ctx)
	defer prepareCancel()
	stopPrepareSession := context.AfterFunc(owner.Session, prepareCancel)
	defer stopPrepareSession()
	stopPreparePolicy := context.AfterFunc(owner.Policy, prepareCancel)
	defer stopPreparePolicy()
	if !owner.alive() {
		return Challenge{}, ErrAttempt
	}
	ctx = prepareCtx
	if m.managed {
		if _, err = m.resolveBinary(ctx, true); err != nil {
			return Challenge{}, err
		}
	}
	if err = m.ready(ctx); err != nil {
		return Challenge{}, err
	}
	if m.pending != nil {
		return Challenge{}, ErrBusy
	}
	account, err := m.readAccount(ctx, false)
	if err != nil {
		return Challenge{}, err
	}
	if account.Authenticated {
		return Challenge{}, ErrConnected
	}
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return Challenge{}, ErrUnavailable
	}
	if err = m.profile.markCleanup(); err != nil {
		return Challenge{}, err
	}
	id := hex.EncodeToString(token[:])
	p := &loginAttempt{owner: owner, state: LoginState{ID: id, State: "pending", ExpiresAt: time.Now().UTC().Add(m.loginTTL)}}
	m.pending = p
	m.last = nil
	abort := func() { m.abort(id) }
	p.stops = append(p.stops, context.AfterFunc(owner.Session, abort), context.AfterFunc(owner.Policy, abort))
	timer := time.AfterFunc(m.loginTTL, abort)
	p.stops = append(p.stops, timer.Stop)
	var raw json.RawMessage
	startCtx, startCancel := context.WithDeadline(ctx, p.state.ExpiresAt)
	stopSession := context.AfterFunc(owner.Session, startCancel)
	stopPolicy := context.AfterFunc(owner.Policy, startCancel)
	if !owner.alive() {
		startCancel()
	}
	err = m.client.call(startCtx, "account/login/start", params, &raw)
	stopSession()
	stopPolicy()
	startCancel()
	if err == nil {
		var challenge LoginChallenge
		challenge, err = loginChallenge(raw)
		if err == nil && challenge.Method == method && owner.alive() && time.Now().Before(p.state.ExpiresAt) {
			p.nativeID = challenge.ID
			challenge.ID = id
			return Challenge{LoginChallenge: challenge, ExpiresAt: p.state.ExpiresAt}, nil
		}
		err = ErrAttempt
	}
	// The caller did not receive a usable challenge. Do not leave its login
	// process active after a dropped/cancelled HTTP request.
	cleanupCtx, cancel := context.WithTimeout(m.life, 5*time.Second)
	defer cancel()
	_ = m.reset(cleanupCtx, "failed")
	return Challenge{}, err
}

func (m *Manager) CancelLogin(ctx context.Context, ownerID, id string) error {
	if !m.mu.TryLock() {
		return ErrBusy
	}
	defer m.mu.Unlock()
	if m.closed || m.pending == nil || m.pending.owner.ID != ownerID || m.pending.state.ID != id {
		return ErrAttempt
	}
	ctx, release := m.operation(ctx)
	defer release()
	return m.reset(ctx, "canceled")
}
func (m *Manager) Logout(ctx context.Context) error {
	if !m.mu.TryLock() {
		return ErrBusy
	}
	if m.closed || m.disconnecting {
		m.mu.Unlock()
		return ErrUnavailable
	}
	m.disconnecting = true
	run := m.active
	if run != nil {
		run.cancel()
	}
	m.mu.Unlock()
	if run != nil {
		<-run.done
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() { m.disconnecting = false }()
	if m.closed {
		return ErrUnavailable
	}
	if m.pending == nil {
		m.last = nil
		m.lastOwner = ""
	}
	ctx, release := m.operation(ctx)
	defer release()
	return m.reset(ctx, "canceled")
}
func (m *Manager) abort(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.pending == nil || m.pending.state.ID != id {
		return
	}
	state := "canceled"
	if !time.Now().Before(m.pending.state.ExpiresAt) {
		state = "expired"
	}
	ctx, cancel := context.WithTimeout(m.life, 5*time.Second)
	defer cancel()
	_ = m.reset(ctx, state)
}

func (m *Manager) watch(c *client) {
	defer m.workers.Done()
	for {
		select {
		case <-c.done:
			m.mu.Lock()
			if !m.closed && m.client == c {
				m.finish("failed")
				// A pending cleanup bit stays durable for the next request.
				m.stopClient()
			}
			m.mu.Unlock()
			return
		case event := <-c.events:
			m.mu.Lock()
			active := m.active
			if active != nil && active.client == c {
				select {
				case active.events <- event:
				default:
					active.cancel()
				}
				m.mu.Unlock()
				continue
			}
			m.mu.Unlock()
			// Outside an active run there are no native request handlers.
			// Refuse unexpected requests by stopping the process.
			if len(event.ID) > 0 {
				c.stop()
				continue
			}
			if event.Method != "account/login/completed" {
				continue
			}
			m.mu.Lock()
			m.complete(c, event.Params)
			m.mu.Unlock()
		}
	}
}
func (m *Manager) complete(c *client, raw json.RawMessage) {
	if m.closed || m.client != c || m.pending == nil {
		return
	}
	var completed struct {
		ID      string `json:"loginId"`
		Success *bool  `json:"success"`
	}
	if json.Unmarshal(raw, &completed) != nil || completed.ID != m.pending.nativeID {
		return
	}
	ctx, cancel := context.WithTimeout(m.life, 10*time.Second)
	defer cancel()
	p := m.pending
	if completed.Success != nil && *completed.Success && p.owner.alive() && time.Now().Before(p.state.ExpiresAt) {
		account, err := m.readAccount(ctx, false)
		if err == nil && account.Authenticated && m.profile.check() == nil && p.owner.alive() && time.Now().Before(p.state.ExpiresAt) {
			if m.profile.clearCleanup() == nil {
				m.finish("connected")
				return
			}
		}
	}
	_ = m.reset(ctx, "failed")
}

// Close never removes a completed account. An unfinished operation leaves its
// cleanup bit for the next manager to resolve through native logout.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.cancel()
		m.mu.Lock()
		m.closed = true
		run := m.active
		if run != nil {
			run.cancel()
		}
		m.mu.Unlock()
		if run != nil {
			<-run.done
		}
		m.mu.Lock()
		m.finish("canceled")
		m.stopClient()
		m.profile.close()
		m.mu.Unlock()
		m.workers.Wait()
	})
}
