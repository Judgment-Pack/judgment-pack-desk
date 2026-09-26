package desk

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

func (s *Server) registerSignIn() {
	s.mux.HandleFunc("GET /api/auth/status", s.handleSignInStatus)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleSignInSetup)
	s.mux.HandleFunc("GET /api/auth/settings", s.handleSignInSettings)
	s.mux.HandleFunc("POST /api/auth/test", s.handleSignInStart)
	s.mux.HandleFunc("POST /api/auth/start", s.handleSignInStart)
	s.mux.HandleFunc("GET /api/auth/callback", s.handleSignInCallback)
	s.mux.HandleFunc("POST /api/auth/complete", s.handleSignInComplete)
	s.mux.HandleFunc("POST /api/auth/enable", s.handleSignInEnable)
}
func (s *Server) signInCallbackURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/api/auth/callback", s.cfg.Port)
}
func signInReply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, status, body)
}
func signInFailure(w http.ResponseWriter, status int, code string) {
	// Error codes are fixed and localized by Desk. Upstream bodies, tokens,
	// callback parameters and registration credentials are never reflected.
	signInReply(w, status, map[string]string{"error": code})
}
func (s *Server) signInBrowser(w http.ResponseWriter, r *http.Request) (string, bool) {
	origin := r.Header.Get("Origin")
	u, parseErr := url.Parse(origin)
	localOrigin := parseErr == nil && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") && u.Port() == fmt.Sprint(s.cfg.Port)
	if s.cfg.DevMode {
		for _, allowed := range devOrigins {
			localOrigin = localOrigin || origin == allowed
		}
	}
	if !localOrigin || origin == "" || !s.originAllowed(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		signInFailure(w, 403, "origin")
		return "", false
	}
	// originAllowed binds the scheme and authority to Desk or the explicit Vite
	// development allowlist. Store that exact origin, never a return URL supplied
	// by the caller or an untrusted forwarded host.
	return origin, true
}
func signInJSON(w http.ResponseWriter, r *http.Request, to any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		signInFailure(w, 415, "invalid-settings")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(to) != nil || decoder.Decode(new(any)) != io.EOF {
		signInFailure(w, 400, "invalid-settings")
		return false
	}
	return true
}
func authProof(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func (a *signInState) pruneLocked() {
	now := time.Now()
	for id, attempt := range a.attempts {
		if !now.Before(attempt.expires) {
			delete(a.attempts, id)
		}
	}
	for admin, id := range a.lastTest {
		if _, ok := a.attempts[id]; !ok {
			delete(a.lastTest, admin)
		}
	}
}
func (a *signInState) admitLocked() bool {
	a.pruneLocked()
	if time.Since(a.limitStart) > time.Minute {
		a.limitStart = time.Now()
		a.limitCount = 0
	}
	if a.limitCount >= 40 || len(a.attempts) >= 32 {
		return false
	}
	a.limitCount++
	return true
}
func (s *Server) handleSignInStatus(w http.ResponseWriter, r *http.Request) {
	a := s.signIn
	a.mu.Lock()
	defer a.mu.Unlock()
	label := ""
	if a.active != nil {
		label = a.active.Provider.Label
	}
	signInReply(w, 200, map[string]any{"enabled": a.active != nil, "label": label, "unavailable": a.problem, "localAccess": s.cfg.LocalAccess && a.active == nil && !a.problem})
}
func (s *Server) handleSignInSetup(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.signInBrowser(w, r); !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !signInJSON(w, r, &body) {
		return
	}
	a := s.signIn
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.problem || a.active != nil {
		signInFailure(w, 403, "setup-disabled")
		return
	}
	if !a.admitLocked() {
		signInFailure(w, 429, "busy")
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Code), []byte(s.cfg.Token)) != 1 {
		signInFailure(w, 403, "setup-code")
		return
	}
	id, err := s.sessions.create("local user", nil)
	if err != nil {
		signInFailure(w, 503, "busy")
		return
	}
	signInReply(w, 200, map[string]string{"id": id})
}
func (s *Server) signInAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.guard(w, r) {
		return "", false
	}
	if _, ok := s.sessionOf(r); !ok {
		signInFailure(w, 401, "session-ended")
		return "", false
	}
	return s.sessions.handle(bearerOf(r)), true
}
func (s *Server) handleSignInSettings(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.signInAdmin(w, r)
	if !ok {
		return
	}
	a := s.signIn
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked()
	out := map[string]any{"enabled": a.active != nil, "callbackUrl": s.signInCallbackURL(), "storageAvailable": s.assistant.usable()}
	if a.active != nil {
		cfg := a.active.Provider
		cfg.Secret = ""
		out["provider"] = cfg
		out["owner"] = a.active.Owner
		out["hasSecret"] = a.active.Provider.Secret != ""
	}
	if attempt := a.attempts[a.lastTest[admin]]; attempt != nil {
		cfg := attempt.provider
		cfg.Secret = ""
		out["provider"] = cfg
		out["hasSecret"] = attempt.provider.Secret != ""
		if attempt.tested && attempt.problem == "" {
			out["testedOwner"] = attempt.owner
			out["testId"] = attempt.id
		}
	}
	signInReply(w, 200, out)
}
func (s *Server) handleSignInStart(w http.ResponseWriter, r *http.Request) {
	origin, ok := s.signInBrowser(w, r)
	if !ok {
		return
	}
	testing := r.URL.Path == "/api/auth/test"
	admin := ""
	if testing {
		admin, ok = s.signInAdmin(w, r)
		if !ok {
			return
		}
	}
	var body struct {
		Provider   *signInProvider `json:"provider,omitempty"`
		ReturnPath string          `json:"returnPath,omitempty"`
		KeepSecret bool            `json:"keepSecret,omitempty"`
	}
	if !signInJSON(w, r, &body) {
		return
	}
	ret := body.ReturnPath
	if ret == "" {
		ret = "/"
	}
	u, err := url.Parse(ret)
	if err != nil || !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") || strings.ContainsAny(ret, "\\\r\n") || u.Host != "" || u.Scheme != "" || strings.HasPrefix(u.Path, "/api/") || strings.HasPrefix(u.Path, "/launch") || len(ret) > 2048 {
		signInFailure(w, 400, "invalid-settings")
		return
	}
	if testing {
		ret = "/admin#identity-provider"
	}
	a := s.signIn
	a.mu.Lock()
	if a.problem || (!testing && a.active == nil) {
		a.mu.Unlock()
		signInFailure(w, 503, "setup-required")
		return
	}
	if !a.admitLocked() {
		a.mu.Unlock()
		signInFailure(w, 429, "busy")
		return
	}
	select {
	case a.slots <- struct{}{}:
	default:
		a.mu.Unlock()
		signInFailure(w, 429, "busy")
		return
	}
	defer func() { <-a.slots }()
	var cfg signInProvider
	if testing {
		if body.Provider == nil || !s.assistant.usable() {
			a.mu.Unlock()
			signInFailure(w, 400, "invalid-settings")
			return
		}
		cfg = *body.Provider
		if body.KeepSecret && cfg.Secret == "" {
			previous := a.active
			if p := a.attempts[a.lastTest[admin]]; p != nil && p.provider.Issuer == cfg.Issuer && p.provider.ClientID == cfg.ClientID {
				cfg.Secret = p.provider.Secret
			} else if previous != nil && previous.Provider.Issuer == cfg.Issuer && previous.Provider.ClientID == cfg.ClientID {
				cfg.Secret = previous.Provider.Secret
			}
		}
	} else {
		cfg = a.active.Provider
	}
	generation := a.generation
	a.mu.Unlock()
	if cfg.validate() != nil {
		signInFailure(w, 400, "invalid-settings")
		return
	}
	protocol, err := discoverSignIn(r.Context(), cfg, s.signInCallbackURL())
	if err != nil {
		signInFailure(w, 502, "provider-unavailable")
		return
	}
	id, err := NewToken()
	if err != nil {
		signInFailure(w, 500, "unavailable")
		return
	}
	proof, err := NewToken()
	if err != nil {
		signInFailure(w, 500, "unavailable")
		return
	}
	state, err := NewToken()
	if err != nil {
		signInFailure(w, 500, "unavailable")
		return
	}
	nonce, err := NewToken()
	if err != nil {
		signInFailure(w, 500, "unavailable")
		return
	}
	attempt := &signInAttempt{id: id, state: state, proof: authProof(proof), nonce: nonce, verifier: oauth2.GenerateVerifier(), origin: origin, returnPath: ret, admin: admin, expires: time.Now().Add(signInAttemptLifetime), generation: generation, provider: cfg, protocol: protocol}
	a.mu.Lock()
	if a.generation != generation || len(a.attempts) >= 32 {
		a.mu.Unlock()
		signInFailure(w, 409, "settings-changed")
		return
	}
	if testing {
		if old := a.lastTest[admin]; old != "" {
			delete(a.attempts, old)
		}
		a.lastTest[admin] = id
	}
	a.attempts[id] = attempt
	a.mu.Unlock()
	signInReply(w, 200, map[string]string{"attemptId": id, "proof": proof, "authorizationUrl": protocol.authorize(state, nonce, attempt.verifier)})
}

func (s *Server) handleSignInCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > 16384 || len(q["state"]) != 1 || len(q["code"]) > 1 || len(q["error"]) > 1 || len(q["iss"]) > 1 {
		signInFailure(w, 400, "invalid-callback")
		return
	}
	a := s.signIn
	a.mu.Lock()
	a.pruneLocked()
	var attempt *signInAttempt
	for _, candidate := range a.attempts {
		if candidate.state == q.Get("state") {
			attempt = candidate
			break
		}
	}
	if attempt == nil || attempt.processing || attempt.finished || attempt.generation != a.generation {
		a.mu.Unlock()
		signInFailure(w, 400, "invalid-callback")
		return
	}
	attempt.processing = true
	a.mu.Unlock()
	var owner signInOwner
	problem := ""
	if q.Get("error") == "access_denied" {
		problem = "cancelled"
	} else if q.Get("error") != "" || q.Get("code") == "" || (q.Get("iss") != "" && q.Get("iss") != attempt.provider.Issuer) {
		problem = "invalid-callback"
	} else {
		var err error
		owner, err = attempt.protocol.exchange(r.Context(), q.Get("code"), attempt.nonce, attempt.verifier)
		if err != nil {
			problem = "verification-failed"
		}
	}
	a.mu.Lock()
	if attempt.generation != a.generation || a.attempts[attempt.id] != attempt || !time.Now().Before(attempt.expires) {
		problem = "settings-changed"
	}
	if problem == "" && attempt.admin == "" && (a.active == nil || a.active.Owner.Subject != owner.Subject || a.active.Owner.Issuer != owner.Issuer) {
		problem = "access-denied"
	}
	attempt.finished = true
	attempt.owner = owner
	attempt.problem = problem
	a.mu.Unlock()
	// This opaque ID is not a session or completion credential. Completion needs
	// the separately held, tab-bound proof and (for a test) the initiating owner.
	http.Redirect(w, r, attempt.origin+"/auth/return#attempt="+url.QueryEscape(attempt.id), http.StatusSeeOther)
}
func (s *Server) handleSignInComplete(w http.ResponseWriter, r *http.Request) {
	origin, ok := s.signInBrowser(w, r)
	if !ok {
		return
	}
	var body struct {
		ID    string `json:"attemptId"`
		Proof string `json:"proof"`
	}
	if !signInJSON(w, r, &body) {
		return
	}
	a := s.signIn
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked()
	attempt := a.attempts[body.ID]
	if attempt == nil || attempt.consumed || attempt.origin != origin || attempt.generation != a.generation || subtle.ConstantTimeCompare([]byte(attempt.proof), []byte(authProof(body.Proof))) != 1 {
		signInFailure(w, 400, "invalid-callback")
		return
	}
	if !attempt.finished {
		signInFailure(w, 409, "pending")
		return
	}
	if attempt.admin != "" {
		held, live := s.sessions.lookup(bearerOf(r))
		if !live || s.sessions.handle(bearerOf(r)) != attempt.admin || held.ctx.Err() != nil {
			signInFailure(w, 401, "session-ended")
			return
		}
	}
	attempt.consumed = true
	if attempt.problem != "" {
		signInFailure(w, 403, attempt.problem)
		return
	}
	if attempt.admin != "" {
		attempt.tested = true
		signInReply(w, 200, map[string]string{"kind": "test", "returnPath": attempt.returnPath})
		return
	}
	if a.active == nil || a.active.Owner.Subject != attempt.owner.Subject || a.active.Owner.Issuer != attempt.owner.Issuer {
		signInFailure(w, 403, "access-denied")
		return
	}
	id, err := s.sessions.createVerified(attempt.owner)
	if err != nil {
		signInFailure(w, 503, "busy")
		return
	}
	signInReply(w, 200, map[string]string{"kind": "login", "id": id, "returnPath": attempt.returnPath})
}
func (s *Server) handleSignInEnable(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.signInBrowser(w, r); !ok {
		return
	}
	admin, ok := s.signInAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		ID string `json:"testId"`
	}
	if !signInJSON(w, r, &body) {
		return
	}
	a := s.signIn
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneLocked()
	attempt := a.attempts[body.ID]
	if attempt == nil || !attempt.tested || attempt.admin != admin || a.lastTest[admin] != attempt.id || attempt.generation != a.generation {
		signInFailure(w, 409, "test-required")
		return
	}
	if held, live := s.sessions.lookup(bearerOf(r)); !live || held.ctx.Err() != nil {
		signInFailure(w, 401, "session-ended")
		return
	}
	record := signInRecord{Version: 1, Provider: attempt.provider, Owner: attempt.owner}
	if err := s.assistant.writeSignIn(record); err != nil {
		signInFailure(w, 500, "storage")
		return
	}
	a.active = &record
	a.generation++
	a.attempts = make(map[string]*signInAttempt)
	a.lastTest = make(map[string]string)
	// Old setup/local sessions and any previous provider sessions all end here.
	// The next ordinary sign-in proves the enabled configuration works as required.
	a.cancelEpoch()
	a.epoch, a.cancelEpoch = context.WithCancel(context.Background())
	s.sessions.close()
	signInReply(w, 200, map[string]bool{"enabled": true})
}
