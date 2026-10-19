package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

const testPassword = "desk4雪"
const nextTestPassword = "desk5678"

func setTestPassword(t *testing.T, ts *httptest.Server, session string) {
	t.Helper()
	code, body := authCall(t, ts, "POST", "/api/auth/password/set", session, map[string]string{"newPassword": testPassword})
	if code != 200 || body["saved"] != true {
		t.Fatalf("password setup failed: %d %v", code, body)
	}
}
func passwordLogin(t *testing.T, ts *httptest.Server, password string) string {
	t.Helper()
	code, body := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": password})
	if code != 200 {
		t.Fatalf("password login failed: %d %v", code, body)
	}
	return body["id"].(string)
}
func TestPasswordSetupLoginChangeAndRevoke(t *testing.T) {
	s, ts := newTestServer(t, false)
	setup := beginSession(t, ts)
	old, _ := s.sessions.lookup(setup)
	epoch := s.signIn.epoch
	setTestPassword(t, ts, setup)
	if old.ctx.Err() == nil || epoch.Err() == nil {
		t.Fatal("setup did not end old session and admitted work")
	}
	_, status := authCall(t, ts, "GET", "/api/auth/status", "", nil)
	if status["mode"] != "password" || status["enabled"] != true || status["localAccess"] != false {
		t.Fatal(status)
	}
	for _, id := range []string{setup, testToken} {
		if code, _ := authCall(t, ts, "GET", "/api/files", id, nil); code != 401 {
			t.Fatal("legacy credential bypassed password")
		}
	}
	for _, path := range []string{"/api/session", "/api/auth/setup"} {
		if code, _ := authCall(t, ts, "POST", path, testToken, map[string]string{"code": testToken}); code < 400 {
			t.Fatal("bootstrap bypassed password")
		}
	}
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": "incorrect password"}); code != 403 {
		t.Fatal("wrong password admitted")
	}
	id := passwordLogin(t, ts, testPassword)
	other := passwordLogin(t, ts, testPassword)
	held, _ := s.sessions.lookup(id)
	if !held.password || held.issuer != nil || held.absolute.IsZero() || held.expires.IsZero() {
		t.Fatal("password session lacks independent method/expiry")
	}
	_, settings := authCall(t, ts, "GET", "/api/auth/settings", id, nil)
	raw, _ := json.Marshal(settings)
	if strings.Contains(string(raw), "argon2") || strings.Contains(string(raw), "salt") || strings.Contains(string(raw), "hash") {
		t.Fatal("settings disclosed password verifier")
	}
	code, body := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": "wrong password", "newPassword": nextTestPassword})
	if code != 403 || body["error"] != "password-current-incorrect" {
		t.Fatal("current password was not required")
	}
	if code, _ := authCall(t, ts, "GET", "/api/session", id, nil); code != 200 {
		t.Fatal("failed change revoked live session")
	}
	code, body = authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": testPassword, "newPassword": nextTestPassword})
	if code != 200 || body["saved"] != true {
		t.Fatalf("change failed %d %v", code, body)
	}
	for _, previous := range []string{id, other} {
		if code, _ := authCall(t, ts, "GET", "/api/session", previous, nil); code != 401 {
			t.Fatal("change left session active")
		}
	}
	if held.ctx.Err() == nil {
		t.Fatal("change did not cancel work")
	}
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": testPassword}); code != 403 {
		t.Fatal("old password still works")
	}
	passwordLogin(t, ts, nextTestPassword)
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": nextTestPassword, "newPassword": testPassword}); code != 401 {
		t.Fatal("stale session changed password")
	}
}
func TestPasswordLocalBypassAndProviderSeparation(t *testing.T) {
	s, ts := localAccessDesk(t, false)
	_, body := localBootstrap(t, s, ts, "")
	setTestPassword(t, ts, body["id"].(string))
	if code, _ := localBootstrap(t, s, ts, ""); code != 401 {
		t.Fatal("local access bypassed password")
	}
	id := passwordLogin(t, ts, testPassword)
	_, session := authCall(t, ts, "GET", "/api/session", id, nil)
	if session["localAccess"] == true {
		t.Fatal("password session advertised automatic access")
	}
	if code, _ := authCall(t, ts, "POST", "/api/auth/start", "", map[string]string{}); code < 400 {
		t.Fatal("OIDC start admitted password mode")
	}
	if code, _ := authCall(t, ts, "POST", "/api/auth/test", id, map[string]any{}); code < 400 {
		t.Fatal("OIDC could replace password without reauthentication")
	}
	other, otherTS := newTestServer(t, false)
	issuer := newTestIssuer(t)
	enableTestIdentity(t, other, otherTS, issuer)
	attempt := authStart(t, otherTS, issuer, "")
	authCallback(t, attempt)
	_, result := authComplete(t, otherTS, attempt, "")
	if code, _ := authCall(t, otherTS, "POST", "/api/auth/password/set", result["id"].(string), map[string]string{"newPassword": testPassword}); code != 409 {
		t.Fatal("password replaced enabled provider")
	}
	if code, _ := authCall(t, otherTS, "POST", "/api/auth/password/login", "", map[string]string{"password": testPassword}); code != 403 {
		t.Fatal("password bypassed provider")
	}
}
func TestPasswordStorageReloadAndCorruptPolicy(t *testing.T) {
	s, ts := newTestServer(t, false)
	setTestPassword(t, ts, beginSession(t, ts))
	path := filepath.Join(s.configDir, secretsDirName, signInFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 || strings.Contains(string(raw), testPassword) {
		t.Fatal("password not stored privately as hash")
	}
	first, err := s.assistant.readSignIn()
	if err != nil || !first.valid() {
		t.Fatal("cannot read password policy")
	}
	cancel := s.signIn.cancelEpoch
	s.openSignIn()
	cancel()
	id := passwordLogin(t, ts, testPassword)
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": testPassword, "newPassword": testPassword}); code != 200 {
		t.Fatal("rehash failed")
	}
	second, _ := s.assistant.readSignIn()
	if first.Password.Salt == second.Password.Salt || first.Password.Hash == second.Password.Hash {
		t.Fatal("salt reused")
	}
	second.Password.Memory = 1 << 31
	malformed, _ := json.Marshal(second)
	if err := os.WriteFile(path, malformed, 0600); err != nil {
		t.Fatal(err)
	}
	cancel = s.signIn.cancelEpoch
	s.openSignIn()
	cancel()
	if !s.SignInRequired() || !s.signIn.problem {
		t.Fatal("malformed policy failed open")
	}
	for _, path := range []string{"/api/auth/setup", "/api/auth/password/login", "/api/session"} {
		if code, _ := authCall(t, ts, "POST", path, testToken, map[string]string{}); code < 400 {
			t.Fatal("corrupt policy admitted access")
		}
	}
}
func TestPasswordValidationOriginsAndBounds(t *testing.T) {
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	for _, value := range []string{"", "abc", "雪雪雪", "123456789", strings.Repeat("雪", 9), strings.Repeat("雪", 129)} {
		if code, _ := authCall(t, ts, "POST", "/api/auth/password/set", id, map[string]string{"newPassword": value}); code != 400 {
			t.Fatal("invalid length accepted")
		}
	}
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/set", testToken, map[string]string{"newPassword": testPassword}); code != 401 {
		t.Fatal("bare setup token changed password")
	}
	for _, path := range []string{"set", "change", "login"} {
		req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/auth/password/"+path, strings.NewReader(`{"newPassword":"not-used","password":"not-used"}`))
		req.Header.Set("Origin", "https://attacker.invalid")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+id)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("cross-origin password request admitted")
		}
	}
	for _, raw := range []string{`{"newPassword":"` + testPassword + `","unexpected":true}`, strings.Repeat(" ", 17000) + `{}`} {
		req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/auth/password/set", strings.NewReader(raw))
		req.Header.Set("Origin", ts.URL)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+id)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal("unbounded/unknown JSON accepted")
		}
	}
	if s.SignInRequired() {
		t.Fatal("invalid requests wrote policy")
	}
	if !validPassword(" 雪 🔑 ") || !validPassword(strings.Repeat("雪", 8)) {
		t.Fatal("Unicode characters or spaces rejected")
	}
	credential := newPassword("   long passphrase   ")
	if !credential.matches("   long passphrase   ") || credential.matches("long passphrase") {
		t.Fatal("password trimmed")
	}
}
func TestPasswordRateLimitAndHashConcurrency(t *testing.T) {
	s, ts := newTestServer(t, false)
	setTestPassword(t, ts, beginSession(t, ts))
	s.signIn.passwordSlots <- struct{}{}
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": testPassword}); code != 429 {
		t.Fatal("concurrent hash admitted")
	}
	<-s.signIn.passwordSlots
	s.signIn.mu.Lock()
	s.signIn.limitStart = time.Now()
	s.signIn.limitCount = 40
	s.signIn.mu.Unlock()
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": testPassword}); code != 429 {
		t.Fatal("rate limit bypassed")
	}
}

func TestPasswordFailedSavePreservesCredentialAndSessions(t *testing.T) {
	s, ts := newTestServer(t, false)
	setTestPassword(t, ts, beginSession(t, ts))
	id := passwordLogin(t, ts, testPassword)
	before := s.signIn.active
	// Make the destination a directory so the atomic replacement fails after
	// hashing and staging, without touching the retained credential itself.
	path := filepath.Join(s.configDir, secretsDirName, signInFileName)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	code, body := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": testPassword, "newPassword": nextTestPassword})
	if code != 500 || body["error"] != "storage" || s.signIn.active != before {
		t.Fatal("failed save activated password")
	}
	if code, _ := authCall(t, ts, "GET", "/api/session", id, nil); code != 200 {
		t.Fatal("failed write revoked session")
	}
	passwordLogin(t, ts, testPassword)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordOperatorResetPreservesWorkspace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project := t.TempDir()
	sentinel := filepath.Join(project, "existing-notes.txt")
	if err := os.WriteFile(sentinel, []byte("keep existing workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	config := configDirFor("")
	s, ts := startDesk(t, Config{ProjectDir: project, DeskConfigDir: config, Token: testToken})
	setTestPassword(t, ts, beginSession(t, ts))
	ts.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ResetSignIn(); err != nil {
		t.Fatal(err)
	}
	reopened, reopenedTS := startDesk(t, Config{ProjectDir: project, DeskConfigDir: config, Token: testToken})
	defer reopenedTS.Close()
	defer reopened.Close()
	if reopened.SignInRequired() {
		t.Fatal("local reset did not remove password policy")
	}
	if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != "keep existing workspace" {
		t.Fatal("local reset touched workspace")
	}
	code, body := authCall(t, reopenedTS, "POST", "/api/auth/setup", "", map[string]string{"code": testToken})
	if code != 200 {
		t.Fatal("owner cannot set up after reset")
	}
	setTestPassword(t, reopenedTS, body["id"].(string))
	passwordLogin(t, reopenedTS, testPassword)
}

// New local passwords accept both limits, count Unicode characters and never
// silently truncate. Existing passphrases remain usable for sign-in and change.
func TestPasswordShortLocalCredentials(t *testing.T) {
	for _, value := range []string{"1234", "simple88", "雪🔑🪴🌙", strings.Repeat("🔑", 8)} {
		if !validPassword(value) {
			t.Fatal("supported local password rejected")
		}
	}
	if validPassword(string([]byte{0xff, 0xff, 0xff, 0xff})) {
		t.Fatal("invalid UTF-8 accepted")
	}
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/set", id, map[string]string{"newPassword": "1234"}); code != 200 {
		t.Fatal("four-character setup refused")
	}
	id = passwordLogin(t, ts, "1234")
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": "1234", "newPassword": "123456789"}); code != 400 {
		t.Fatal("overlong replacement accepted")
	}
	passwordLogin(t, ts, "1234")
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": "1234", "newPassword": "simple88"}); code != 200 {
		t.Fatal("eight-character replacement refused")
	}
	passwordLogin(t, ts, "simple88")
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": "1234"}); code != 403 {
		t.Fatal("replaced password remains usable")
	}
}

func TestPasswordLegacyPassphraseCanSignInAndChange(t *testing.T) {
	s, ts := newTestServer(t, false)
	legacy := strings.Repeat("🔑", 128)
	// Install a previous version's persisted credential in this disposable fixture.
	if err := s.assistant.writeSignIn(signInRecord{Version: 2, Password: newPassword(legacy)}); err != nil {
		t.Fatal(err)
	}
	cancel := s.signIn.cancelEpoch
	s.openSignIn()
	cancel()
	id := passwordLogin(t, ts, legacy)
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": legacy, "newPassword": "1234"}); code != 200 {
		t.Fatal("existing passphrase could not be changed")
	}
	passwordLogin(t, ts, "1234")
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/login", "", map[string]string{"password": legacy}); code != 403 {
		t.Fatal("old passphrase remains usable")
	}
}

// A completed AI account belongs to the installation, not a browser login.
type passwordConnectedAccount struct{ accountStub }

func (a *passwordConnectedAccount) Status(ctx context.Context, owner string, refresh bool) (codexbridge.Status, error) {
	status, err := a.accountStub.Status(ctx, owner, refresh)
	status.Account = "connected"
	return status, err
}
func TestPasswordChangesPreserveCompletedAIConnection(t *testing.T) {
	s, ts := newTestServer(t, false)
	if s.codex != nil {
		s.codex.Close()
	}
	account := &passwordConnectedAccount{}
	s.codex = account
	setup := beginSession(t, ts)
	setTestPassword(t, ts, setup)
	id := passwordLogin(t, ts, testPassword)
	check := func(id string, priorCalls int) {
		t.Helper()
		if account.closed || account.calls != priorCalls {
			t.Fatal("password change mutated the AI account")
		}
		code, body := authCall(t, ts, "GET", "/api/model-providers/openai/status", id, nil)
		if code != 200 || body["account"] != "connected" || account.refresh {
			t.Fatal("password session lost its existing AI connection")
		}
	}
	check(id, 0)
	if code, _ := authCall(t, ts, "POST", "/api/auth/password/change", id, map[string]string{"currentPassword": testPassword, "newPassword": nextTestPassword}); code != 200 {
		t.Fatal("password change failed")
	}
	check(passwordLogin(t, ts, nextTestPassword), 1)
}

func TestPasswordPublicStatusChecksOptionalSessionWithoutMinting(t *testing.T) {
	s, ts := newTestServer(t, false)
	setup := beginSession(t, ts)
	setTestPassword(t, ts, setup)
	valid := passwordLogin(t, ts, testPassword)
	wrongPolicy, err := s.sessions.create("local user", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := s.sessions.count()
	for _, tc := range []struct {
		bearer string
		active bool
	}{{"", false}, {testToken, false}, {"forged", false}, {setup, false}, {wrongPolicy, false}, {valid, true}} {
		code, body := authCall(t, ts, "GET", "/api/auth/status", tc.bearer, nil)
		if code != 200 || body["sessionActive"] != tc.active || body["mode"] != "password" {
			t.Fatal(code, body)
		}
		for _, private := range []string{"id", "subject", "issuer", "name", "email", "password"} {
			if _, present := body[private]; present {
				t.Fatal("public status exposed identity or credentials", private)
			}
		}
	}
	if s.sessions.count() != before {
		t.Fatal("status minted a session")
	}
	s.sessions.revoke(valid)
	_, status := authCall(t, ts, "GET", "/api/auth/status", valid, nil)
	if status["sessionActive"] != false {
		t.Fatal("revoked session still reported active")
	}
	// Public status is descriptive only; protected routes still reject stale IDs.
	if code, _ := authCall(t, ts, "GET", "/api/session", valid, nil); code != 401 {
		t.Fatal("status bypassed session guard", code)
	}
	if code, _ := authCall(t, ts, "POST", "/api/session", valid, nil); code != 401 {
		t.Fatal("password bypass through legacy exchange", code)
	}
}
