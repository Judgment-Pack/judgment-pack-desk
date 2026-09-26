package desk

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const signInSessionLifetime = 8 * time.Hour
const signInIdleLifetime = 30 * time.Minute

func (st *sessionStore) createVerified(owner signInOwner) (string, error) {
	return st.createExpiring(owner.Subject, &owner.Issuer, owner.Name, owner.Email)
}

func (st *sessionStore) createExpiring(subject string, issuer *string, name, email string) (string, error) {
	id, err := st.create(subject, issuer)
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	handle := st.handle(id)
	held, live := st.live[handle]
	if !live {
		st.mu.Unlock()
		return "", errors.New("session ended during sign-in")
	}
	held.name = name
	held.email = email
	held.absolute = time.Now().Add(signInSessionLifetime)
	held.expires = time.Now().Add(signInIdleLifetime)
	timer := time.AfterFunc(signInIdleLifetime, func() { st.expireVerified(handle) })
	held.timer = timer
	cancel := held.cancel
	held.cancel = func() { timer.Stop(); cancel() }
	st.live[handle] = held
	st.mu.Unlock()
	return id, nil
}
func (st *sessionStore) expireVerified(handle string) {
	st.mu.Lock()
	held, ok := st.live[handle]
	if !ok {
		st.mu.Unlock()
		return
	}
	remaining := time.Until(held.expires)
	if remaining > 0 {
		held.timer.Reset(remaining)
		st.mu.Unlock()
		return
	}
	delete(st.live, handle)
	st.mu.Unlock()
	held.cancel()
}

// A policy change cancels admitted work, including legacy script requests
// whose launch credential has no browser-session context. Activation's own
// response is exempt so it can report the durable transition to its caller.
func (s *Server) withSignInPolicy(r *http.Request) (*http.Request, func()) {
	if s.signIn == nil || r.URL.Path == "/api/auth/enable" {
		return r, func() {}
	}
	s.signIn.mu.Lock()
	epoch := s.signIn.epoch
	s.signIn.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(epoch, cancel)
	if epoch.Err() != nil {
		cancel()
	}
	return r.WithContext(ctx), func() { stop(); cancel() }
}
