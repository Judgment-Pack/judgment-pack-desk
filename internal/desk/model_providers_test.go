package desk

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

type accountStub struct {
	mu      sync.Mutex
	calls   int
	owner   codexbridge.Owner
	refresh bool
	err     error
	closed  bool
}

func (a *accountStub) Status(_ context.Context, _ string, refresh bool) (codexbridge.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.refresh = refresh
	return codexbridge.Status{Provider: "openai", AuthMethod: "subscription", Agent: "codex", Runtime: "available", Account: "signed-out"}, a.err
}
func (a *accountStub) StartLogin(_ context.Context, o codexbridge.Owner, _ string) (codexbridge.Challenge, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.owner = o
	return codexbridge.Challenge{LoginChallenge: codexbridge.LoginChallenge{ID: "attempt", Method: "browser", URL: "https://auth.openai.com/oauth/authorize?state=transient"}}, a.err
}
func (a *accountStub) CancelLogin(_ context.Context, _ string, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.err
}
func (a *accountStub) Logout(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.err
}
func (a *accountStub) Close() { a.mu.Lock(); defer a.mu.Unlock(); a.closed = true }
func providerRequest(s *Server, method, path, token, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8791"+path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func providerTestServer(t *testing.T) (*Server, *accountStub, string) {
	t.Helper()
	s, err := New(Config{ProjectDir: t.TempDir(), JpackBin: jpackBinary(), DeskConfigDir: t.TempDir(), Port: 8791, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	id, err := s.sessions.create("local-user", nil)
	if err != nil {
		t.Fatal(err)
	}
	stub := &accountStub{}
	if s.codex != nil {
		s.codex.Close()
	}
	s.codex = stub
	return s, stub, id
}

func TestModelProviderRequiresSessionAndOrigin(t *testing.T) {
	s, stub, token := providerTestServer(t)
	for _, tc := range []struct {
		token, origin string
		status        int
	}{
		{"", "", 401}, {testToken, "", 401}, {token, "https://untrusted.invalid", 403}, {token, "", 403},
	} {
		w := providerRequest(s, "POST", "/api/model-providers/openai/login", tc.token, tc.origin, `{"method":"browser"}`)
		if w.Code != tc.status {
			t.Fatalf("guard = %d, want %d: %s", w.Code, tc.status, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable auth response")
		}
	}
	if stub.calls != 0 {
		t.Fatal("unauthorized provider operation")
	}
	w := providerRequest(s, "GET", "/api/model-providers/openai/status", token, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"account":"signed-out"`) {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
}

func TestModelProviderOnlyAcceptsClosedRequests(t *testing.T) {
	s, stub, token := providerTestServer(t)
	for _, tc := range []struct{ method, action, body string }{
		{"POST", "login", `{"method":"apiKey"}`},
		{"POST", "login", `{"method":"browser","apiKey":"PRIVATE_SENTINEL"}`},
		{"POST", "login", `{"method":"browser","cwd":"/project"}`},
		{"POST", "login", `{"method":"browser","binary":"/tmp/other"}`},
		{"POST", "login", `{"method":"browser"}{}`},
		{"POST", "login", `{"method":"` + strings.Repeat("a", 2048) + `"}`},
		{"POST", "cancel", `{"id":""}`},
		{"POST", "logout", `{"account":"other"}`},
		{"POST", "refresh", `{"endpoint":"https://untrusted.invalid"}`},
		{"GET", "logout", ""},
		{"POST", "status", `{}`},
		{"POST", "initialize", `{}`},
		{"GET", "status?refreshToken=true", ""},
	} {
		w := providerRequest(s, tc.method, "/api/model-providers/openai/"+tc.action, token, "http://127.0.0.1:8791", tc.body)
		if w.Code < 400 {
			t.Fatalf("accepted %s %s: %s", tc.method, tc.action, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "PRIVATE_SENTINEL") {
			t.Fatal("reflected private request")
		}
	}
	if stub.calls != 0 {
		t.Fatalf("invalid requests reached manager: %d", stub.calls)
	}
}

func TestProviderLoginOwnerOutlivesRequestButEndsWithSession(t *testing.T) {
	s, stub, token := providerTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "http://127.0.0.1:8791/api/model-providers/openai/login", strings.NewReader(`{"method":"browser"}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "http://127.0.0.1:8791")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	cancel()
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	if stub.owner.ID == token || stub.owner.ID == "" || stub.owner.Session.Err() != nil || stub.owner.Policy.Err() != nil {
		t.Fatal("wrong login owner or request-scoped lifetime")
	}
	s.sessions.revoke(token)
	if stub.owner.Session.Err() == nil {
		t.Fatal("session revocation did not cancel login owner")
	}
}
func TestProviderPolicyChangeCancelsLoginOwner(t *testing.T) {
	s, stub, token := providerTestServer(t)
	w := providerRequest(s, "POST", "/api/model-providers/openai/login", token, "http://127.0.0.1:8791", `{"method":"browser"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.signIn.mu.Lock()
	s.signIn.cancelEpoch()
	s.signIn.mu.Unlock()
	if stub.owner.Policy.Err() == nil {
		t.Fatal("policy revocation did not reach login")
	}
}
func TestModelProviderFailuresAreRedactedAndShutdownClosesManager(t *testing.T) {
	s, stub, token := providerTestServer(t)
	stub.err = errors.New("PRIVATE_SENTINEL")
	w := providerRequest(s, "POST", "/api/model-providers/openai/refresh", token, "http://127.0.0.1:8791", `{}`)
	if w.Code != 503 || strings.Contains(w.Body.String(), "PRIVATE") || !stub.refresh {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	stub.err = codexbridge.ErrBusy
	w = providerRequest(s, "POST", "/api/model-providers/openai/logout", token, "http://127.0.0.1:8791", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "provider-busy") {
		t.Fatalf("busy: %d %s", w.Code, w.Body.String())
	}
	_ = s.Close()
	if !stub.closed {
		t.Fatal("account manager not closed")
	}
}
func TestModelProviderDisabledDoesNotRegisterRunnableEngine(t *testing.T) {
	s, _, token := providerTestServer(t)
	s.codex = nil
	w := providerRequest(s, "GET", "/api/model-providers", token, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"engineReady":false`) || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
	}
	w = providerRequest(s, "GET", "/api/model-providers/openai/status", token, "", "")
	if w.Code != 503 {
		t.Fatalf("disabled provider status: %d", w.Code)
	}
}

type modelAccountStub struct {
	accountStub
	modelErr error
}

func (a *modelAccountStub) Models(context.Context) ([]codexbridge.Model, error) {
	return []codexbridge.Model{{ID: "model", Name: "Account model", Efforts: []string{"medium"}, DefaultEffort: "medium"}}, a.modelErr
}
func TestProviderModelsUseTheGuardAndClosedResponse(t *testing.T) {
	s, _, token := providerTestServer(t)
	s.codex = &modelAccountStub{}
	path := "/api/model-providers/openai/models"
	response := providerRequest(s, "GET", path, token, "http://127.0.0.1:8791", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"defaultEffort":"medium"`) {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("catalog can be cached")
	}
	if got := providerRequest(s, "GET", path, "", "http://127.0.0.1:8791", ""); got.Code != 401 {
		t.Fatalf("anonymous models: %d", got.Code)
	}
	if got := providerRequest(s, "POST", path, token, "http://127.0.0.1:8791", "{}"); got.Code != 405 {
		t.Fatalf("mutation accepted: %d", got.Code)
	}
	s.codex = &modelAccountStub{modelErr: codexbridge.ErrSignIn}
	response = providerRequest(s, "GET", path, token, "http://127.0.0.1:8791", "")
	if response.Code != 409 || strings.TrimSpace(response.Body.String()) != `{"error":"sign-in-required"}` {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
}

func TestModelProviderManagedByDefaultWithoutNativeStartup(t *testing.T) {
	if !codexbridge.ManagedRuntimeSupported() {
		t.Skip("unsupported managed host")
	}
	dir := t.TempDir()
	s, err := New(Config{ProjectDir: t.TempDir(), JpackBin: jpackBinary(), DeskConfigDir: dir, Port: 8791, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	token, err := s.sessions.create("local-user", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := providerRequest(s, "GET", "/api/model-providers", token, "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatal("managed runtime disabled", response.Body.String())
	}
	if _, err = os.Stat(filepath.Join(dir, "codex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("catalog acquired a profile", err)
	}
	response = providerRequest(s, "GET", "/api/model-providers/openai/status", token, "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"runtime":"not-installed"`) {
		t.Fatal("unexpected status", response.Code, response.Body.String())
	}
	if _, err = os.Stat(filepath.Join(dir, "codex", "runtime")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("passive status prepared runtime", err)
	}
}
func TestModelProviderInstallationCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{ProjectDir: t.TempDir(), JpackBin: jpackBinary(), DeskConfigDir: dir, Port: 8791, Token: testToken, CodexBin: "off"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	token, _ := s.sessions.create("local-user", nil)
	response := providerRequest(s, "GET", "/api/model-providers", token, "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"availability":"disabled"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if _, err = os.Stat(filepath.Join(dir, "codex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disabled installation created profile", err)
	}
}
