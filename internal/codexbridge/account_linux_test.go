package codexbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This subprocess emulates native account methods against synthetic state.
// It never reads a user's auth.json or makes an upstream request.
func TestAccountHelper(t *testing.T) {
	if os.Getenv("JPS_ACCOUNT_HELPER") != "1" {
		return
	}
	mode := os.Getenv("JPS_ACCOUNT_MODE")
	dir := os.Getenv("CODEX_HOME")
	fixture := filepath.Join(dir, "account.fixture")
	var mu sync.Mutex
	send := func(value any) { mu.Lock(); defer mu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(value) }
	complete := func(id string, success bool) {
		if success {
			kind := "chatgpt"
			if mode == "wrong-type" {
				kind = "apiKey"
			}
			_ = os.WriteFile(fixture, []byte(kind), 0600)
		}
		send(map[string]any{"method": "account/login/completed", "params": map[string]any{"loginId": id, "success": success, "error": "PRIVATE_SENTINEL"}})
	}
	go func() {
		for {
			time.Sleep(5 * time.Millisecond)
			path := filepath.Join(os.Getenv("TMPDIR"), "event")
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			_ = os.Remove(path)
			switch string(data) {
			case "complete":
				complete("native-id", true)
			case "fail":
				complete("native-id", false)
			case "stale":
				send(map[string]any{"method": "account/login/completed", "params": map[string]any{"loginId": "unrelated-id", "success": true}})
			case "exit":
				os.Exit(0)
			case "request":
				send(map[string]any{"id": "native-request", "method": "item/tool/call", "params": map[string]string{"name": "exec_command"}})
			}
		}
	}()
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req frame
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(1)
		}
		reply := func(v any) { send(map[string]any{"id": req.ID, "result": v}) }
		fail := func() {
			send(map[string]any{"id": req.ID, "error": map[string]any{"code": -1, "message": "PRIVATE_SENTINEL"}})
		}
		switch req.Method {
		case "initialize":
			reply(map[string]string{"userAgent": "jps_desk/0.157.1 (Linux)"})
		case "initialized":
		case "account/read":
			var params struct {
				Refresh bool `json:"refreshToken"`
			}
			_ = json.Unmarshal(req.Params, &params)
			if params.Refresh && mode == "refresh-fail" {
				fail()
				continue
			}
			var account any
			if data, err := os.ReadFile(fixture); err == nil {
				account = map[string]string{"type": string(data), "planType": "plus", "email": "PRIVATE_SENTINEL", "accessToken": "PRIVATE_SENTINEL"}
			}
			reply(map[string]any{"requiresOpenaiAuth": true, "account": account})
		case "account/login/start":
			if mode == "hang-login" {
				time.Sleep(time.Minute)
				continue
			}
			var params struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(req.Params, &params)
			if mode == "bad-challenge" {
				reply(map[string]string{"type": "chatgpt", "loginId": "native-id", "authUrl": "https://untrusted.invalid/"})
				continue
			}
			if params.Type == "chatgptDeviceCode" {
				reply(map[string]string{"type": params.Type, "loginId": "native-id", "verificationUrl": "https://auth.openai.com/codex/device", "userCode": "ABCD-EFGH"})
			} else {
				reply(map[string]string{"type": "chatgpt", "loginId": "native-id", "authUrl": "https://auth.openai.com/oauth/authorize?state=transient"})
			}
			if mode == "success" || mode == "wrong-type" {
				complete("native-id", true)
			}
		case "account/login/cancel":
			// A completion racing cancellation really writes synthetic state.
			// Only the fresh process logout can remove it reliably.
			if mode == "cancel-late" {
				complete("native-id", true)
			}
			reply(map[string]string{"status": "canceled"})
		case "account/logout":
			if mode == "logout-fail" {
				fail()
				continue
			}
			_ = os.Remove(fixture)
			reply(map[string]any{})
		case "model/list":
			var params struct {
				IncludeHidden bool `json:"includeHidden"`
			}
			_ = json.Unmarshal(req.Params, &params)
			rows := catalogRows()
			foreign := func(slug string, hidden bool) map[string]any {
				return map[string]any{"model": slug, "displayName": slug, "hidden": hidden,
					"defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "medium"}}}
			}
			switch mode {
			case "catalog-mismatch":
				// A visible model in place of one of Desk's.
				rows = append(rows[1:], foreign("gpt-5.2", false))
			case "catalog-subset":
				rows = rows[1:]
			case "catalog-duplicate":
				// One of Desk's models twice, another missing: the same count.
				rows = append(rows[1:], rows[1])
			case "catalog-hidden-extras":
				// The release's own catalog: Desk's models plus hidden ones, which
				// only a listing that asks for hidden models reveals.
				if params.IncludeHidden {
					for _, slug := range []string{"gpt-daybreak-blue-latest", "gpt-daybreak-red-latest", "gpt-5.4", "codex-auto-review"} {
						rows = append(rows, foreign(slug, true))
					}
				}
			}
			reply(map[string]any{"data": rows})
		default:
			fail()
		}
	}
	os.Exit(0)
}

// A process that lists any model outside Desk's catalog did not apply it, so
// no account or run operation may use that process, and it is reaped.
func TestAProcessListingOtherModelsIsRefused(t *testing.T) {
	for _, mode := range []string{"catalog-mismatch", "catalog-subset", "catalog-duplicate", "catalog-hidden-extras"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := accountManager(t, mode)
			defer m.Close()
			var launched *client
			launch := m.launch
			m.launch = func(ctx context.Context, native *exec.Cmd) (*client, error) {
				c, err := launch(ctx, native)
				launched = c
				return c, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := m.Status(ctx, "test", false); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("status through a process with a foreign model list: %v", err)
			}
			if m.client != nil {
				t.Fatal("the refused process was kept")
			}
			if launched == nil {
				t.Fatal("no process was launched")
			}
			select {
			case <-launched.done:
			case <-time.After(3 * time.Second):
				t.Fatal("the refused process is still running")
			}
		})
	}
}

func accountManager(t *testing.T, mode string) (*Manager, Options) {
	t.Helper()
	options := Options{Binary: os.Args[0], ProfileDir: filepath.Join(t.TempDir(), "codex"), ProjectDir: t.TempDir()}
	m := reopenAccountManager(t, options, mode)
	return m, options
}
func reopenAccountManager(t *testing.T, options Options, mode string) *Manager {
	t.Helper()
	m, err := NewManager(options)
	if err != nil {
		info, _ := os.Stat(filepath.Dir(options.ProfileDir))
		t.Fatalf("profile: %v parent %v", err, info)
	}
	m.launch = func(ctx context.Context, native *exec.Cmd) (*client, error) {
		helper := exec.Command(os.Args[0], "-test.run=^TestAccountHelper$")
		helper.Env = append(native.Env, "JPS_ACCOUNT_HELPER=1", "JPS_ACCOUNT_MODE="+mode)
		helper.Dir = native.Dir
		return startClient(ctx, helper)
	}
	t.Cleanup(m.Close)
	return m
}
func ownerFixture() (Owner, context.CancelFunc, context.CancelFunc) {
	session, cancelSession := context.WithCancel(context.Background())
	policy, cancelPolicy := context.WithCancel(context.Background())
	return Owner{ID: "session-handle", Session: session, Policy: policy}, cancelSession, cancelPolicy
}
func awaitAccount(t *testing.T, m *Manager, account, login string) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, err := m.Status(context.Background(), "session-handle", false)
		if err == nil && s.Account == account && (login == "" || s.Login != nil && s.Login.State == login) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("account never reached %s / %s", account, login)
	return Status{}
}
func signalAccount(t *testing.T, m *Manager, event string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.profile.temp, "event"), []byte(event), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAccountCompletesPersistsAndLogsOut(t *testing.T) {
	m, options := accountManager(t, "success")
	o, sessionEnd, policyEnd := ownerFixture()
	defer sessionEnd()
	defer policyEnd()
	awaitAccount(t, m, "signed-out", "")
	challenge, err := m.StartLogin(context.Background(), o, "browser")
	if err != nil || challenge.ID == "native-id" || challenge.ID == "" || challenge.URL == "" {
		t.Fatalf("challenge: %+v %v", challenge, err)
	}
	status := awaitAccount(t, m, "connected", "connected")
	wire, _ := json.Marshal(status)
	if status.Plan != "plus" || status.LastVerified == nil || strings.Contains(string(wire), "PRIVATE") || strings.Contains(string(wire), "native-id") || strings.Contains(string(wire), "transient") {
		t.Fatalf("bad status: %s", wire)
	}
	if _, err = m.StartLogin(context.Background(), o, "device"); !errors.Is(err, ErrConnected) {
		t.Fatalf("replaced account: %v", err)
	}
	m.Close()
	m = reopenAccountManager(t, options, "pending")
	awaitAccount(t, m, "connected", "")
	if err = m.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := awaitAccount(t, m, "signed-out", ""); status.Login != nil {
		t.Fatal("logout retained a connected login indicator")
	}
	m.Close()
	m = reopenAccountManager(t, options, "pending")
	awaitAccount(t, m, "signed-out", "")
}

func TestLoginIsSessionBoundAndCancelRejectsLateCompletion(t *testing.T) {
	m, _ := accountManager(t, "cancel-late")
	o, a, b := ownerFixture()
	defer a()
	defer b()
	challenge, err := m.StartLogin(context.Background(), o, "device")
	if err != nil || challenge.Code != "ABCD-EFGH" {
		t.Fatalf("device: %+v %v", challenge, err)
	}
	if _, err = m.StartLogin(context.Background(), o, "browser"); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent login: %v", err)
	}
	status, err := m.Status(context.Background(), "other-session", false)
	if err != nil || status.Login != nil || status.Account != "login-pending" {
		t.Fatalf("cross-session status: %+v %v", status, err)
	}
	for _, input := range [][2]string{{"other-session", challenge.ID}, {o.ID, "stale"}} {
		if err = m.CancelLogin(context.Background(), input[0], input[1]); !errors.Is(err, ErrAttempt) {
			t.Fatalf("foreign cancellation: %v", err)
		}
	}
	signalAccount(t, m, "stale")
	time.Sleep(30 * time.Millisecond)
	awaitAccount(t, m, "login-pending", "pending")
	if err = m.CancelLogin(context.Background(), o.ID, challenge.ID); err != nil {
		t.Fatal(err)
	}
	awaitAccount(t, m, "signed-out", "canceled")
	// Old native notifications cannot change the new client's account.
	time.Sleep(30 * time.Millisecond)
	awaitAccount(t, m, "signed-out", "canceled")
}

func TestSessionPolicyExpiryAndNativeExitCancelLogin(t *testing.T) {
	for _, cause := range []string{"session", "policy", "expiry", "exit", "request", "fail"} {
		t.Run(cause, func(t *testing.T) {
			m, _ := accountManager(t, "pending")
			if cause == "expiry" {
				m.loginTTL = 50 * time.Millisecond
			}
			o, sessionEnd, policyEnd := ownerFixture()
			defer sessionEnd()
			defer policyEnd()
			if _, err := m.StartLogin(context.Background(), o, "browser"); err != nil {
				t.Fatal(err)
			}
			expected := "canceled"
			switch cause {
			case "session":
				sessionEnd()
			case "policy":
				policyEnd()
			case "expiry":
				expected = "expired"
			default:
				expected = "failed"
				signalAccount(t, m, cause)
			}
			awaitAccount(t, m, "signed-out", expected)
			cleanup, err := m.profile.needsCleanup()
			if err != nil || cleanup {
				t.Fatalf("cleanup incomplete: %v %v", cleanup, err)
			}
		})
	}
}

func TestPendingLoginAndFailedLogoutRecoverAfterRestart(t *testing.T) {
	for _, mode := range []string{"pending", "logout-fail"} {
		t.Run(mode, func(t *testing.T) {
			m, options := accountManager(t, mode)
			o, a, b := ownerFixture()
			defer a()
			defer b()
			if _, err := m.StartLogin(context.Background(), o, "browser"); err != nil {
				t.Fatal(err)
			}
			// Represent a native credential write just before Desk interruption.
			if err := os.WriteFile(filepath.Join(m.profile.codex, "account.fixture"), []byte("chatgpt"), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "logout-fail" {
				err := m.Logout(context.Background())
				if !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatalf("logout failure: %v", err)
				}
			}
			m.Close()
			m = reopenAccountManager(t, options, "pending")
			awaitAccount(t, m, "signed-out", "")
			cleanup, err := m.profile.needsCleanup()
			if err != nil || cleanup {
				t.Fatalf("cleanup not durable: %v %v", cleanup, err)
			}
		})
	}
}

func TestAccountRejectsWrongTypeAndBadChallenge(t *testing.T) {
	for _, mode := range []string{"wrong-type", "bad-challenge"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := accountManager(t, mode)
			o, a, b := ownerFixture()
			defer a()
			defer b()
			_, err := m.StartLogin(context.Background(), o, "browser")
			if mode == "bad-challenge" && err == nil {
				t.Fatal("untrusted challenge accepted")
			}
			awaitAccount(t, m, "signed-out", "failed")
		})
	}
}

func TestRefreshFailurePreservesAccount(t *testing.T) {
	m, options := accountManager(t, "success")
	o, a, b := ownerFixture()
	defer a()
	defer b()
	if _, err := m.StartLogin(context.Background(), o, "device"); err != nil {
		t.Fatal(err)
	}
	awaitAccount(t, m, "connected", "connected")
	m.Close()
	m = reopenAccountManager(t, options, "refresh-fail")
	if _, err := m.Status(context.Background(), o.ID, true); !errors.Is(err, ErrUpstream) {
		t.Fatalf("refresh failure: %v", err)
	}
	awaitAccount(t, m, "connected", "")
}

func TestCloseInterruptsLoginAndReleasesProfileLease(t *testing.T) {
	m, options := accountManager(t, "hang-login")
	o, a, b := ownerFixture()
	defer a()
	defer b()
	result := make(chan error, 1)
	go func() { _, err := m.StartLogin(context.Background(), o, "browser"); result <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(options.ProfileDir, "cleanup")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	m.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatal("shutdown blocked on pending native RPC")
	}
	if err := <-result; err == nil {
		t.Fatal("interrupted login succeeded")
	}
	m = reopenAccountManager(t, options, "pending")
	awaitAccount(t, m, "signed-out", "")
}

func TestNativePrivateAccountProfile(t *testing.T) {
	binary := os.Getenv("JPS_CODEX_TEST_BINARY")
	if binary == "" {
		t.Skip("set JPS_CODEX_TEST_BINARY for native private-profile check")
	}
	m, err := NewManager(Options{Binary: binary, ProfileDir: filepath.Join(t.TempDir(), "codex"), ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := m.Status(ctx, "test", false)
	if err != nil || s.Account != "signed-out" || s.Runtime != "available" {
		t.Fatalf("private native profile: %+v %v", s, err)
	}
	if err = m.Logout(ctx); err != nil {
		t.Fatalf("native signed-out logout: %v", err)
	}
	// A just-reaped process can briefly own the lifecycle mutex while its
	// notification watcher exits; status callers explicitly support busy.
	awaitAccount(t, m, "signed-out", "")
}

func TestSessionCancellationInterruptsBlockedLogin(t *testing.T) {
	m, _ := accountManager(t, "hang-login")
	owner, cancelSession, cancelPolicy := ownerFixture()
	defer cancelSession()
	defer cancelPolicy()
	done := make(chan error, 1)
	go func() { _, err := m.StartLogin(context.Background(), owner, "browser"); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(m.profile.dir, "cleanup")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancelSession()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked session got a challenge")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revoked session left native login request blocked")
	}
	awaitAccount(t, m, "signed-out", "failed")
}

func TestConcurrentLoginAdmissionHasOneOwner(t *testing.T) {
	m, _ := accountManager(t, "pending")
	owner, a, b := ownerFixture()
	defer a()
	defer b()
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; _, err := m.StartLogin(context.Background(), owner, "device"); results <- err }()
	}
	close(start)
	admitted, busy := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			admitted++
		} else if errors.Is(err, ErrBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if admitted != 1 || busy != 1 {
		t.Fatalf("admitted=%d busy=%d", admitted, busy)
	}
	awaitAccount(t, m, "login-pending", "pending")
}
