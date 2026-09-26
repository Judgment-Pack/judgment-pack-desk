package desk

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func localAccessDesk(t *testing.T, dev bool) (*Server, *httptest.Server) {
	t.Helper()
	s, ts := startDesk(t, Config{ProjectDir: t.TempDir(), JpackBin: jpackBinary(), Token: testToken, LocalAccess: true, DevMode: dev, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { ts.Close(); _ = s.Close() })
	return s, ts
}

func localBootstrap(t *testing.T, s *Server, ts *httptest.Server, id string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest("POST", ts.URL+"/api/session", nil)
	r.RemoteAddr = "127.0.0.1:34567"
	r.Header.Set("Origin", ts.URL)
	r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
	if id != "" {
		r.Header.Set("Authorization", "Bearer "+id)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func TestLocalAccessCreatesBoundedSessionAndReusesItOnReload(t *testing.T) {
	s, ts := localAccessDesk(t, false)
	code, body := localBootstrap(t, s, ts, "")
	if code != 200 {
		t.Fatalf("local bootstrap: %d %v", code, body)
	}
	id := body["id"].(string)
	held, ok := s.sessions.lookup(id)
	if !ok || held.issuer != nil || held.expires.IsZero() || held.absolute.IsZero() {
		t.Fatal("local session lacks expected scope or expiry")
	}
	for range 70 {
		code, body = localBootstrap(t, s, ts, id)
		if code != 200 || body["id"] != id {
			t.Fatal("reload replaced session")
		}
	}
	if s.sessions.count() != 1 {
		t.Fatal("reload consumed session capacity")
	}
	if code, _ := authCall(t, ts, "GET", "/api/files", "", nil); code != 401 {
		t.Fatal("private API became anonymous")
	}
	if code, _ := authCall(t, ts, "GET", "/api/files", id, nil); code != 200 {
		t.Fatal("minted bearer cannot open files")
	}
	code, body = authCall(t, ts, "GET", "/api/session", id, nil)
	if code != 200 || body["localAccess"] != true {
		t.Fatal("session does not identify local access")
	}
	s.sessions.revoke(id)
	code, body = localBootstrap(t, s, ts, id)
	if code != 200 || body["id"] == id {
		t.Fatal("revoked credential was reused")
	}
}

func TestLocalAccessRequiresLoopbackAndExactBrowserOrigin(t *testing.T) {
	s, ts := localAccessDesk(t, false)
	for _, row := range []struct{ name, origin, site, host, remote string }{
		{"no origin", "", "same-origin", "", "127.0.0.1:42"},
		{"no metadata", ts.URL, "", "", "127.0.0.1:42"},
		{"cross site", ts.URL, "cross-site", "", "127.0.0.1:42"},
		{"sibling origin", "http://localhost:9000", "same-origin", "", "127.0.0.1:42"},
		{"DNS rebinding", "http://attacker.example", "same-origin", "attacker.example", "127.0.0.1:42"},
		{"remote", ts.URL, "same-origin", "", "192.0.2.2:42"},
		{"Vite outside dev", "http://localhost:5173", "same-origin", "", "127.0.0.1:42"},
	} {
		t.Run(row.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", ts.URL+"/api/session", nil)
			r.RemoteAddr = row.remote
			if row.host != "" {
				r.Host = row.host
			}
			r.Header.Set("Origin", row.origin)
			r.Header.Set(fetchSiteHeader, row.site)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || s.sessions.count() != 0 {
				t.Fatalf("unexpected access: %d", w.Code)
			}
		})
	}
}

func TestLocalAccessAcceptsExplicitViteOriginOnlyInDev(t *testing.T) {
	s, ts := localAccessDesk(t, true)
	r := httptest.NewRequest("POST", ts.URL+"/api/session", nil)
	r.RemoteAddr = "127.0.0.1:42"
	r.Header.Set("Origin", "http://localhost:5173")
	r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("Vite refused: %d", w.Code)
	}
}

func TestLocalAccessNeverOverridesEnabledOrUnreadableIdentityPolicy(t *testing.T) {
	s, ts := localAccessDesk(t, false)
	_, first := localBootstrap(t, s, ts, "")
	p := newTestIssuer(t)
	enableTestIdentity(t, s, ts, p)
	code, _ := localBootstrap(t, s, ts, first["id"].(string))
	if code != 401 {
		t.Fatal("environment option bypassed OIDC")
	}
	_, status := authCall(t, ts, "GET", "/api/auth/status", "", nil)
	if status["localAccess"] != false || status["enabled"] != true {
		t.Fatal("status advertised a bypass")
	}
	attempt := authStart(t, ts, p, "")
	authCallback(t, attempt)
	if code, _ = authComplete(t, ts, attempt, ""); code != 200 {
		t.Fatal("owner OIDC login was blocked")
	}
	s.signIn.mu.Lock()
	s.signIn.active = nil
	s.signIn.problem = true
	s.signIn.mu.Unlock()
	if code, _ = localBootstrap(t, s, ts, ""); code != 401 {
		t.Fatal("unreadable policy unlocked access")
	}
}

func TestLocalAccessIsOptIn(t *testing.T) {
	s, ts := newTestServer(t, false)
	if code, _ := localBootstrap(t, s, ts, ""); code != 401 {
		t.Fatal("local access enabled without opt-in")
	}
}
