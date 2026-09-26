package desk

import (
	"net"
	"net/http"
)

// Explicit local access trusts the OS account and a same-origin browser on
// loopback. It still issues a private bearer; other routes never become public.
func (s *Server) beginLocalSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || r.Header.Get(fetchSiteHeader) != fetchSiteSameOrigin {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "local access requires this computer's own browser")
		return
	}
	if _, ok := s.signInBrowser(w, r); !ok {
		return
	}
	a := s.signIn
	a.mu.Lock()
	defer a.mu.Unlock()
	// Check and mint under the policy lock, so activation cannot race a new
	// local session into existence after it revokes all old sessions.
	if a.active != nil || a.problem {
		writeJSONCoded(w, http.StatusUnauthorized, CodeNoHandoff, "sign-in required")
		return
	}
	if id := bearerOf(r); id != "" {
		if held, live := s.sessions.lookup(id); live && held.issuer == nil && held.ctx.Err() == nil {
			writeJSON(w, http.StatusOK, map[string]string{"id": id})
			return
		}
	}
	id, err := s.sessions.createExpiring("local user", nil, "", "")
	if err == errTooManySessions {
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeSessionsFull, errTooManySessions.Error())
		return
	}
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "this desk could not mint a session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}
