package desk

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Version 2 is local password authentication only; existing version 1 OIDC
// policies remain unchanged. One immutable credential per installation.
type passwordCredential struct {
	Algorithm   string `json:"algorithm"`
	Version     int    `json:"version"`
	Memory      uint32 `json:"memoryKiB"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	Salt        string `json:"salt"`
	Hash        string `json:"hash"`
}

const passwordMemory uint32 = 19 * 1024
const passwordIterations uint32 = 2

func (p *passwordCredential) valid() bool {
	if p == nil || p.Algorithm != "argon2id" || p.Version != argon2.Version || p.Memory != passwordMemory || p.Iterations != passwordIterations || p.Parallelism != 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(p.Salt)
	hash, errHash := base64.RawStdEncoding.Strict().DecodeString(p.Hash)
	return err == nil && errHash == nil && len(salt) == 16 && len(hash) == 32
}
func (r signInRecord) valid() bool {
	if r.Version == 2 {
		return r.Password.valid() && r.Provider == (signInProvider{}) && r.Owner == (signInOwner{})
	}
	return r.Version == 1 && r.Password == nil && r.Provider.validate() == nil && r.Owner.Subject != "" && len(r.Owner.Subject) <= 512 && r.Owner.Issuer == r.Provider.Issuer
}
func validPassword(value string) bool {
	// New local passwords use 4–8 Unicode characters. Do not trim, normalize
	// or truncate input; verification separately supports older passphrases.
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 4 && utf8.RuneCountInString(value) <= 8
}
func newPassword(value string) *passwordCredential {
	salt := make([]byte, 16)
	// crypto/rand.Read cannot return a recoverable error in our Go toolchain.
	_, _ = rand.Read(salt)
	hash := argon2.IDKey([]byte(value), salt, passwordIterations, passwordMemory, 1, 32)
	return &passwordCredential{Algorithm: "argon2id", Version: argon2.Version, Memory: passwordMemory, Iterations: passwordIterations, Parallelism: 1, Salt: base64.RawStdEncoding.EncodeToString(salt), Hash: base64.RawStdEncoding.EncodeToString(hash)}
}
func (p *passwordCredential) matches(value string) bool {
	// Keep previously saved longer passwords usable for sign-in and change.
	if !p.valid() || !utf8.ValidString(value) || utf8.RuneCountInString(value) < 4 || utf8.RuneCountInString(value) > 128 {
		return false
	}
	salt, _ := base64.RawStdEncoding.DecodeString(p.Salt)
	expected, _ := base64.RawStdEncoding.DecodeString(p.Hash)
	actual := argon2.IDKey([]byte(value), salt, p.Iterations, p.Memory, p.Parallelism, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
func (a *signInState) modeLocked() string {
	if a.active == nil {
		return "setup"
	}
	if a.active.Password != nil {
		return "password"
	}
	return "provider"
}
func signInPolicyMutation(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/auth/enable" || r.URL.Path == "/api/auth/password/set" || r.URL.Path == "/api/auth/password/change")
}

// Only one expensive password calculation runs at once. Rate limiting is
// installation-wide, and hashing never holds the policy lock needed by guards.
func (a *signInState) admitPasswordLocked() bool {
	if !a.admitLocked() {
		return false
	}
	select {
	case a.passwordSlots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.signInBrowser(w, r); !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !signInJSON(w, r, &body) {
		return
	}
	a := s.signIn
	a.mu.Lock()
	if a.problem || a.modeLocked() != "password" {
		a.mu.Unlock()
		signInFailure(w, 403, "password-mode-changed")
		return
	}
	if !a.admitPasswordLocked() {
		a.mu.Unlock()
		signInFailure(w, 429, "busy")
		return
	}
	record, generation := a.active, a.generation
	a.mu.Unlock()
	defer func() { <-a.passwordSlots }()
	matched := record.Password.matches(body.Password)
	body.Password = ""
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Context().Err() != nil || a.problem || a.generation != generation || a.active != record {
		signInFailure(w, 409, "password-mode-changed")
		return
	}
	if !matched {
		signInFailure(w, 403, "password-incorrect")
		return
	}
	id, err := s.sessions.createPassword()
	if err != nil {
		signInFailure(w, 503, "busy")
		return
	}
	signInReply(w, 200, map[string]string{"id": id})
}
func (s *Server) handlePasswordSave(w http.ResponseWriter, r *http.Request) {
	s.signIn.policySourceMu.Lock()
	defer s.signIn.policySourceMu.Unlock()
	if _, ok := s.signInBrowser(w, r); !ok {
		return
	}
	if _, ok := s.signInAdmin(w, r); !ok {
		return
	}
	var body struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if !signInJSON(w, r, &body) {
		return
	}
	if !validPassword(body.New) {
		signInFailure(w, 400, "password-length")
		return
	}
	changing := r.URL.Path == "/api/auth/password/change"
	a := s.signIn
	a.mu.Lock()
	mode := a.modeLocked()
	if a.problem || (changing && mode != "password") || (!changing && mode != "setup") {
		a.mu.Unlock()
		signInFailure(w, 409, "password-mode-changed")
		return
	}
	if !a.admitPasswordLocked() {
		a.mu.Unlock()
		signInFailure(w, 429, "busy")
		return
	}
	record, generation := a.active, a.generation
	a.mu.Unlock()
	defer func() { <-a.passwordSlots }()
	if changing && !record.Password.matches(body.Current) {
		signInFailure(w, 403, "password-current-incorrect")
		return
	}
	credential := newPassword(body.New)
	body.Current, body.New = "", ""
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Context().Err() != nil || a.problem || a.generation != generation || a.active != record {
		signInFailure(w, 409, "password-mode-changed")
		return
	}
	if held, ok := s.sessions.lookup(bearerOf(r)); !ok || held.ctx.Err() != nil {
		signInFailure(w, 401, "session-ended")
		return
	}
	next := signInRecord{Version: 2, Password: credential}
	if err := s.assistant.writeSignIn(next); err != nil {
		signInFailure(w, 500, "storage")
		return
	}
	a.active = &next
	a.generation++
	a.attempts = make(map[string]*signInAttempt)
	a.lastTest = make(map[string]string)
	a.cancelEpoch()
	a.epoch, a.cancelEpoch = context.WithCancel(context.Background())
	s.sessions.close()
	signInReply(w, 200, map[string]bool{"saved": true})
}
func (st *sessionStore) createPassword() (string, error) {
	id, err := st.createExpiring("local user", nil, "", "")
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	held, ok := st.live[st.handle(id)]
	if !ok {
		st.mu.Unlock()
		return "", errors.New("session ended during sign-in")
	}
	held.password = true
	st.live[st.handle(id)] = held
	st.mu.Unlock()
	return id, nil
}
