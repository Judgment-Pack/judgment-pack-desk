package desk

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestEndSessionRevokesOnlyThePresentedSession(t *testing.T) {
	s, ts := newTestServer(t, false)
	id, other := beginSession(t, ts), beginSession(t, ts)
	held, _ := s.sessions.lookup(id)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
	pageBearer(id)(req)
	req.Header.Set("Origin", ts.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("end session: %d", resp.StatusCode)
	}
	if _, ok := s.sessions.lookup(id); ok {
		t.Fatal("revoked session remains live")
	}
	if held.ctx.Err() == nil {
		t.Fatal("session work was not canceled")
	}
	if _, ok := s.sessions.lookup(other); !ok {
		t.Fatal("another session was revoked")
	}
	if s.sessions.count() != 1 {
		t.Fatal("revocation did not release store capacity")
	}
	check, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	pageBearer(id)(check)
	resp, err = http.DefaultClient.Do(check)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked credential read: %d", resp.StatusCode)
	}
	// The local launcher remains usable; sign-out is not an installation lock.
	if next := beginSession(t, ts); next == id {
		t.Fatal("relaunch reused the revoked credential")
	}
}

func TestEndSessionRefusesOtherOriginsAndNonSessionCredentials(t *testing.T) {
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	for _, row := range []struct {
		name, token, origin string
		status              int
	}{
		{"cross origin", id, "https://untrusted.example", 403},
		{"no bearer", "", ts.URL, 401},
		{"launch secret", testToken, ts.URL, 401},
		{"forged bearer", "forged", ts.URL, 401},
	} {
		t.Run(row.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
			if row.token != "" {
				pageBearer(row.token)(req)
			}
			req.Header.Set("Origin", row.origin)
			req.AddCookie(&http.Cookie{Name: s.launchCookie, Value: id})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != row.status {
				t.Fatalf("got %d, want %d", resp.StatusCode, row.status)
			}
			if _, ok := s.sessions.lookup(id); !ok {
				t.Fatal("refused request ended session")
			}
		})
	}
}

func TestRevocationCancelsAnAlreadyAdmittedRequest(t *testing.T) {
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	started, stopped := make(chan struct{}), make(chan struct{})
	s.mux.HandleFunc("GET /api/test-session-work", func(w http.ResponseWriter, r *http.Request) {
		if !s.guard(w, r) {
			return
		}
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/test-session-work", nil)
	pageBearer(id)(req)
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("work did not start")
	}
	if !s.sessions.revoke(id) {
		t.Fatal("could not revoke")
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("admitted work survived revocation")
	}
	<-done
}

func TestRevocationClosesAnInitializedRuntimeSocket(t *testing.T) {
	requireBinary(t)
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {ts.URL}}, Subprotocols: upgradeOffer(id)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(readLimit)
	(&rpcSession{t: t, ctx: ctx, ws: c}).initialize()
	if !s.sessions.revoke(id) {
		t.Fatal("could not revoke")
	}
	_, _, err = c.Read(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("socket was not closed by revocation: %v", err)
	}
}

func TestConcurrentSessionRevocationHasOneWinner(t *testing.T) {
	st, err := newSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.create("local user", nil)
	if err != nil {
		t.Fatal(err)
	}
	held, _ := st.lookup(id)
	var wg sync.WaitGroup
	results := make(chan bool, 16)
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- st.revoke(id) }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for ok := range results {
		if ok {
			winners++
		}
	}
	if winners != 1 || held.ctx.Err() == nil || st.count() != 0 {
		t.Fatal("concurrent revocation was not atomic")
	}
}

// Shutdown must also drain a script-authenticated socket, which is not owned
// by a browser session and is no longer tracked by net/http after the upgrade.
func TestShutdownDrainsRuntimeBeforeClosingProject(t *testing.T) {
	requireBinary(t)
	s, ts := newTestServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}, "Origin": {ts.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(readLimit)
	(&rpcSession{t: t, ctx: ctx, ws: c}).initialize()
	ended := make(chan error, 1)
	go func() { ended <- s.Close() }()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown did not drain the runtime")
	}
	s.mu.Lock()
	remaining := len(s.conns)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("shutdown left %d runtime connections", remaining)
	}
	if _, _, err := c.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("socket survived shutdown: %v", err)
	}
}
