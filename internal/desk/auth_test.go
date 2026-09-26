package desk

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	jose "github.com/go-jose/go-jose/v4"
)

type testIssuer struct {
	server  *httptest.Server
	mu      sync.Mutex
	key     *ecdsa.PrivateKey
	subject string
	change  func(map[string]any)
	codes   map[string]url.Values
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := &testIssuer{key: key, subject: "owner-123", codes: map[string]url.Values{}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"ES256"}, "code_challenge_methods_supported": []string{"S256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "test-key", Algorithm: "ES256", Use: "sig"}}})
		case "/authorize":
			q := r.URL.Query()
			code, _ := NewToken()
			p.codes[code] = q
			if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" {
				http.Error(w, "missing PKCE/nonce", 400)
				return
			}
			target, _ := url.Parse(q.Get("redirect_uri"))
			v := target.Query()
			v.Set("state", q.Get("state"))
			v.Set("code", code)
			v.Set("iss", p.server.URL)
			target.RawQuery = v.Encode()
			http.Redirect(w, r, target.String(), 302)
		case "/token":
			_ = r.ParseForm()
			q, ok := p.codes[r.Form.Get("code")]
			delete(p.codes, r.Form.Get("code"))
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || r.Form.Get("client_id") != "test-client" || r.Form.Get("redirect_uri") != q.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
				http.Error(w, "invalid grant", 400)
				return
			}
			claims := map[string]any{"iss": p.server.URL, "sub": p.subject, "aud": "test-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": q.Get("nonce"), "name": "Test owner", "email": "owner@example.test"}
			if p.change != nil {
				p.change(claims)
			}
			raw, _ := json.Marshal(claims)
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: p.key}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
			signed, _ := signer.Sign(raw)
			token, _ := signed.CompactSerialize()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "not-retained", "token_type": "Bearer", "expires_in": 3600, "id_token": token})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}
func authCall(t *testing.T, ts *httptest.Server, method, path, id string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, ts.URL+path, reader)
	req.Header.Set("Origin", ts.URL)
	req.Header.Set("Content-Type", "application/json")
	if id != "" {
		req.Header.Set("Authorization", "Bearer "+id)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return res.StatusCode, result
}
func authStart(t *testing.T, ts *httptest.Server, p *testIssuer, id string) map[string]any {
	t.Helper()
	path := "/api/auth/start"
	body := map[string]any{"returnPath": "/packs"}
	if id != "" {
		path = "/api/auth/test"
		body["provider"] = signInProvider{Label: "Test provider", Issuer: p.server.URL, ClientID: "test-client"}
	}
	status, attempt := authCall(t, ts, "POST", path, id, body)
	if status != 200 {
		t.Fatalf("start: %d %v", status, attempt)
	}
	return attempt
}
func authCallback(t *testing.T, attempt map[string]any) string {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(attempt["authorizationUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	callback := res.Header.Get("Location")
	res, err = client.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 303 || !strings.Contains(res.Header.Get("Location"), "/auth/return#attempt=") {
		t.Fatalf("callback: %d", res.StatusCode)
	}
	return callback
}
func authComplete(t *testing.T, ts *httptest.Server, attempt map[string]any, id string) (int, map[string]any) {
	t.Helper()
	return authCall(t, ts, "POST", "/api/auth/complete", id, map[string]any{"attemptId": attempt["attemptId"], "proof": attempt["proof"]})
}
func enableTestIdentity(t *testing.T, s *Server, ts *httptest.Server, p *testIssuer) string {
	t.Helper()
	id := beginSession(t, ts)
	attempt := authStart(t, ts, p, id)
	authCallback(t, attempt)
	status, body := authComplete(t, ts, attempt, id)
	if status != 200 || body["kind"] != "test" {
		t.Fatalf("test complete: %d %v", status, body)
	}
	status, body = authCall(t, ts, "POST", "/api/auth/enable", id, map[string]any{"testId": attempt["attemptId"]})
	if status != 200 {
		t.Fatalf("enable: %d %v", status, body)
	}
	return id
}
func TestOIDCTestEnableLoginAndOwnerEnforcement(t *testing.T) {
	p := newTestIssuer(t)
	s, ts := newTestServer(t, false)
	_, status := authCall(t, ts, "GET", "/api/auth/status", "", nil)
	if status["enabled"] != false {
		t.Fatal(status)
	}
	old := enableTestIdentity(t, s, ts, p)
	for _, token := range []string{old, testToken} {
		code, _ := authCall(t, ts, "GET", "/api/files", token, nil)
		if code != 401 {
			t.Fatalf("old credential admitted: %d", code)
		}
	}
	code, _ := authCall(t, ts, "POST", "/api/auth/setup", "", map[string]string{"code": testToken})
	if code != 403 {
		t.Fatal("setup code bypassed identity")
	}
	code, _ = authCall(t, ts, "POST", "/api/session", "", nil)
	if code != 401 {
		t.Fatal("launch exchange bypassed identity")
	}
	stored, err := s.assistant.readSignIn()
	if err != nil || stored == nil || stored.Owner.Subject != "owner-123" {
		t.Fatalf("durable policy: %v %v", stored, err)
	}
	p.mu.Lock()
	p.subject = "another-user"
	p.mu.Unlock()
	attempt := authStart(t, ts, p, "")
	authCallback(t, attempt)
	code, result := authComplete(t, ts, attempt, "")
	if code != 403 || result["error"] != "access-denied" {
		t.Fatalf("wrong owner: %d %v", code, result)
	}
	p.mu.Lock()
	p.subject = "owner-123"
	p.mu.Unlock()
	attempt = authStart(t, ts, p, "")
	authCallback(t, attempt)
	code, result = authComplete(t, ts, attempt, "")
	if code != 200 || result["kind"] != "login" {
		t.Fatalf("owner login: %d %v", code, result)
	}
	newID := result["id"].(string)
	code, result = authCall(t, ts, "GET", "/api/session", newID, nil)
	if code != 200 || result["subject"] != "owner-123" || result["issuer"] != p.server.URL || result["name"] != "Test owner" {
		t.Fatalf("session: %d %v", code, result)
	}
	held, _ := s.sessions.lookup(newID)
	if held.absolute.IsZero() || held.timer == nil {
		t.Fatal("OIDC session has no expiry")
	}
	if code, _ = authComplete(t, ts, attempt, ""); code != 400 {
		t.Fatal("completion replay succeeded")
	}
	code, _ = authCall(t, ts, "DELETE", "/api/session", newID, nil)
	if code != 204 {
		t.Fatal("logout failed")
	}
	if held.ctx.Err() == nil {
		t.Fatal("logout did not cancel session work")
	}
}
func TestOIDCProofAndTestRemainBoundToInitiatingSession(t *testing.T) {
	p := newTestIssuer(t)
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	other := beginSession(t, ts)
	attempt := authStart(t, ts, p, id)
	callback := authCallback(t, attempt)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("callback replay accepted")
	}
	wrong := map[string]any{"attemptId": attempt["attemptId"], "proof": "wrong"}
	if code, _ := authCall(t, ts, "POST", "/api/auth/complete", id, wrong); code != 400 {
		t.Fatal("wrong proof accepted")
	}
	if code, _ := authComplete(t, ts, attempt, other); code != 401 {
		t.Fatal("test transferred between sessions")
	}
	if code, _ := authComplete(t, ts, attempt, id); code != 200 {
		t.Fatal("test did not complete")
	}
	held, _ := s.sessions.lookup(id)
	if held.issuer != nil || held.subject != "local user" {
		t.Fatal("test changed administrator identity")
	}
	s.sessions.revoke(id)
	if code, _ := authCall(t, ts, "POST", "/api/auth/enable", id, map[string]any{"testId": attempt["attemptId"]}); code != 401 {
		t.Fatal("ended session enabled identity")
	}
}
func TestOIDCRejectsInvalidIdentityClaims(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"nonce":            func(c map[string]any) { c["nonce"] = "other" },
		"issuer":           func(c map[string]any) { c["iss"] = "https://wrong.example" },
		"audience":         func(c map[string]any) { c["aud"] = "another-client" },
		"expiry":           func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"future-issued":    func(c map[string]any) { c["iat"] = time.Now().Add(time.Hour).Unix() },
		"authorized-party": func(c map[string]any) { c["aud"] = []string{"test-client", "other"}; c["azp"] = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			p := newTestIssuer(t)
			p.change = change
			_, ts := newTestServer(t, false)
			id := beginSession(t, ts)
			attempt := authStart(t, ts, p, id)
			authCallback(t, attempt)
			status, body := authComplete(t, ts, attempt, id)
			if status != 403 || body["error"] != "verification-failed" {
				t.Fatalf("%d %v", status, body)
			}
		})
	}
}
func TestOIDCOriginsAndReturnPaths(t *testing.T) {
	_, ts := newTestServer(t, false)
	for _, origin := range []string{"", "https://evil.example", "http://rebound.example:" + strings.Split(ts.URL, ":")[2]} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/auth/setup", strings.NewReader(`{"code":"`+testToken+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if strings.Contains(origin, "rebound") {
			req.Host = strings.TrimPrefix(origin, "http://")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("origin %q: %d", origin, res.StatusCode)
		}
	}
	for _, path := range []string{"https://evil.example", "//evil.example", "/\\evil.example", "/api/files", "/launch?secret=x"} {
		code, _ := authCall(t, ts, "POST", "/api/auth/start", "", map[string]string{"returnPath": path})
		if code != 400 {
			t.Fatalf("return path %q: %d", path, code)
		}
	}
}
func TestOIDCStorageRefusesMalformedOrPubliclyReadablePolicy(t *testing.T) {
	s, _ := newTestServer(t, false)
	path := filepath.Join(s.configDir, secretsDirName, signInFileName)
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.assistant.readSignIn(); err == nil {
		t.Fatal("malformed policy accepted")
	}
	s.openSignIn()
	if !s.SignInRequired() {
		t.Fatal("malformed policy opened local fallback")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.assistant.readSignIn(); err == nil {
		t.Fatal("public file accepted")
	}
}
func TestOIDCExpiryCancelsSessionAndFreesCapacity(t *testing.T) {
	st, err := newSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	defer st.close()
	id, err := st.createVerified(signInOwner{Subject: "owner", Issuer: "https://issuer.example", Name: "Owner"})
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	held := st.live[st.handle(id)]
	held.expires = time.Now().Add(-time.Second)
	st.live[st.handle(id)] = held
	st.mu.Unlock()
	if _, ok := st.lookup(id); ok {
		t.Fatal("expired session accepted")
	}
	if held.ctx.Err() != context.Canceled || st.count() != 0 {
		t.Fatal("expiry did not release work/capacity")
	}
}
func TestOIDCReusableAcrossIssuers(t *testing.T) {
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p := newTestIssuer(t)
			s, ts := newTestServer(t, false)
			enableTestIdentity(t, s, ts, p)
			attempt := authStart(t, ts, p, "")
			authCallback(t, attempt)
			if status, _ := authComplete(t, ts, attempt, ""); status != 200 {
				t.Fatal("issuer login failed")
			}
		})
	}
}

func TestOIDCActivationCancelsLegacyWorkAndSockets(t *testing.T) {
	requireBinary(t)
	p := newTestIssuer(t)
	s, ts := newTestServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}, "Origin": {ts.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(readLimit)
	(&rpcSession{t: t, ctx: ctx, ws: c}).initialize()
	started, stopped := make(chan struct{}), make(chan struct{})
	s.mux.HandleFunc("GET /api/legacy-work", func(w http.ResponseWriter, r *http.Request) {
		if !s.guard(w, r) {
			return
		}
		close(started)
		<-r.Context().Done()
		close(stopped)
	})
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/legacy-work", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
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
		t.Fatal("legacy request did not start")
	}
	enableTestIdentity(t, s, ts, p)
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("legacy work survived policy change")
	}
	<-done
	if _, _, err = c.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("legacy socket survived activation: %v", err)
	}
}

func TestOIDCExpiredSupersededAndRevokedTestsCannotActivate(t *testing.T) {
	p := newTestIssuer(t)
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	first := authStart(t, ts, p, id)
	authCallback(t, first)
	authComplete(t, ts, first, id)
	second := authStart(t, ts, p, id)
	if code, _ := authCall(t, ts, "POST", "/api/auth/enable", id, map[string]any{"testId": first["attemptId"]}); code != 409 {
		t.Fatal("superseded test enabled")
	}
	authCallback(t, second)
	authComplete(t, ts, second, id)
	s.signIn.mu.Lock()
	s.signIn.attempts[second["attemptId"].(string)].expires = time.Now().Add(-time.Second)
	s.signIn.mu.Unlock()
	if code, _ := authCall(t, ts, "POST", "/api/auth/enable", id, map[string]any{"testId": second["attemptId"]}); code != 409 {
		t.Fatal("expired test enabled")
	}
	third := authStart(t, ts, p, id)
	authCallback(t, third)
	authComplete(t, ts, third, id)
	s.sessions.revoke(id)
	if code, _ := authCall(t, ts, "POST", "/api/auth/enable", id, map[string]any{"testId": third["attemptId"]}); code != 401 {
		t.Fatal("revoked session enabled")
	}
	if s.SignInRequired() {
		t.Fatal("refused enable mutated policy")
	}
}

func TestOIDCPolicyReloadDoesNotPermitLaunchSecret(t *testing.T) {
	p := newTestIssuer(t)
	s, ts := newTestServer(t, false)
	enableTestIdentity(t, s, ts, p)
	// Re-open the durable private policy, as a new process does. No test attempt
	// or ephemeral browser session is necessary to enforce it after restart.
	oldCancel := s.signIn.cancelEpoch
	s.openSignIn()
	oldCancel()
	if !s.SignInRequired() {
		t.Fatal("policy lost on reload")
	}
	if code, _ := authCall(t, ts, "GET", "/api/files", testToken, nil); code != 401 {
		t.Fatal("launch secret admitted after reload")
	}
	attempt := authStart(t, ts, p, "")
	authCallback(t, attempt)
	if code, _ := authComplete(t, ts, attempt, ""); code != 200 {
		t.Fatal("owner cannot log in after reload")
	}
}
