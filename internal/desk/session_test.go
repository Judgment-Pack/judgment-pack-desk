package desk

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/* The exchange -------------------------------------------------------------- */

// TestLaunchExchangeSetsTheSessionCookie is the whole of the browser's entry:
// the printed URL in, a `303` to `/` and one cookie out, and **no secret in
// anything the browser keeps afterwards**.
func TestLaunchExchangeSetsTheSessionCookie(t *testing.T) {
	_, ts := newTestServer(t, false)
	resp := launchResponse(t, ts, testToken)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/" {
		t.Errorf("Location = %q, want %q", got, "/")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("the exchange set %d cookies, want exactly 1: %v", len(cookies), cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "jpack-desk-session" {
		t.Errorf("cookie name = %q, want %q", cookie.Name, "jpack-desk-session")
	}
	if cookie.Value == "" {
		t.Fatal("the cookie carries no session id")
	}
	// The id is minted here and is not the secret that bought it. A desk that
	// set the launch secret as the cookie would have moved the secret into the
	// browser rather than out of the URL.
	if cookie.Value == testToken {
		t.Error("the cookie carries the launch secret itself")
	}
	if len(cookie.Value) != 48 {
		t.Errorf("session id is %d hex characters, want 48 (192 bits)", len(cookie.Value))
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie is not HttpOnly: page code can read the id")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", cookie.SameSite)
	}
	// Not `Secure` on http: a browser discards a Secure cookie that arrives
	// over plain http, and this chassis serves plain http on loopback — so
	// setting it here would mean setting no cookie at all.
	if cookie.Secure {
		t.Error("Secure is set on an http response, so the browser would drop the cookie")
	}
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Errorf("the session cookie is not a session cookie: MaxAge=%d Expires=%v",
			cookie.MaxAge, cookie.Expires)
	}
	// And nothing in the response repeats the secret.
	if strings.Contains(resp.Header.Get("Location"), testToken) {
		t.Error("the redirect carries the launch secret")
	}
}

// TestLaunchRefusesAWrongSecretAndSetsNothing: the refusal is a refusal, not a
// weaker grant.
func TestLaunchRefusesAWrongSecretAndSetsNothing(t *testing.T) {
	s, ts := newTestServer(t, false)
	for _, secret := range []string{
		"",
		"wrong",
		strings.Repeat("f", len(testToken)),
		testToken[:len(testToken)-1],
		testToken + "x",
		strings.ToUpper(testToken),
	} {
		t.Run("secret="+secret, func(t *testing.T) {
			resp := launchResponse(t, ts, secret)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
			}
			if cookies := resp.Cookies(); len(cookies) != 0 {
				t.Fatalf("a refused exchange set %d cookie(s): %v", len(cookies), cookies)
			}
		})
	}
	if n := s.sessions.count(); n != 0 {
		t.Fatalf("%d session(s) were minted by refused exchanges", n)
	}
}

// TestLaunchRefusalIsOneLine pins the body's shape, because a 403 that rendered
// the page would be a 403 that told a person nothing.
func TestLaunchRefusalIsOneLine(t *testing.T) {
	_, ts := newTestServer(t, false)
	resp := launchResponse(t, ts, "wrong")
	defer resp.Body.Close()
	body := make([]byte, 512)
	n, _ := resp.Body.Read(body)
	text := strings.TrimSpace(string(body[:n]))
	if strings.Contains(text, "\n") {
		t.Errorf("the refusal body is more than one line: %q", text)
	}
	if !strings.Contains(text, "launch secret") {
		t.Errorf("the refusal does not say what was wrong: %q", text)
	}
	if strings.Contains(text, testToken) {
		t.Error("the refusal repeats the launch secret")
	}
}

// TestLaunchSecretStaysValidForTheProcess: reopening the printed URL is a
// second cookie and not a restart. Single-use would make every closed tab a
// restart of the desk, for a property this chunk does not claim.
func TestLaunchSecretStaysValidForTheProcess(t *testing.T) {
	s, ts := newTestServer(t, false)
	first := launchSession(t, ts)
	second := launchSession(t, ts)
	if first.Value == second.Value {
		t.Error("two exchanges produced one session id")
	}
	if n := s.sessions.count(); n != 2 {
		t.Errorf("%d live sessions, want 2", n)
	}
	// Both still open the relay.
	for _, cookie := range []*http.Cookie{first, second} {
		resp := upgradeRequest(t, ts, "", ts.URL, withSession(cookie))
		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("a session minted by the launch exchange does not authorize")
		}
	}
}

// TestLaunchIsNotGatedByTheSessionItMints — the route where authorization is
// acquired cannot require the authorization it hands out.
func TestLaunchIsNotGatedByTheSessionItMints(t *testing.T) {
	_, ts := newTestServer(t, false)
	// No cookie, no header, and it still works: that is the point.
	cookie := launchSession(t, ts)
	if cookie.Value == "" {
		t.Fatal("the launch exchange minted nothing")
	}
}

/* The store ------------------------------------------------------------------ */

// TestSessionStoreHoldsNoSessionID is the "never a map lookup on the raw value"
// rule, held over the store itself rather than over the function that reads it.
func TestSessionStoreHoldsNoSessionID(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	minted := map[string]bool{}
	for range 8 {
		id, err := store.create("local user", nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		minted[id] = true
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.live) != len(minted) {
		t.Fatalf("%d entries for %d ids", len(store.live), len(minted))
	}
	for key := range store.live {
		if minted[key] {
			t.Fatalf("the store is keyed by a session id: %q", key)
		}
		// A hex SHA-256, which is what the MAC of an id looks like and what a
		// 192-bit id does not.
		if len(key) != 64 {
			t.Fatalf("key %q is %d characters, want a 64-character MAC", key, len(key))
		}
	}
}

// TestSessionHandleIsNotTheID is the helper's own test: it derives, it is
// stable, and it does not reproduce its input.
func TestSessionHandleIsNotTheID(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	const id = "0123456789abcdef0123456789abcdef0123456789abcdef"
	handle := store.handle(id)
	if handle == id {
		t.Fatal("the handle is the id")
	}
	if strings.Contains(handle, id) {
		t.Fatal("the handle contains the id")
	}
	if store.handle(id) != handle {
		t.Fatal("the handle is not stable for one id")
	}
	// One bit of difference is a whole different handle, which is what stops a
	// near-miss landing in the same bucket as a live session.
	if store.handle(id[:len(id)-1]+"0") == handle {
		t.Fatal("two different ids share a handle")
	}
	// And the key is this process's: two stores disagree about the same id, so
	// a handle observed anywhere is not a handle anywhere else.
	other, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	if other.handle(id) == handle {
		t.Fatal("two stores derive the same handle, so the MAC key is not per process")
	}
}

// TestSessionStoreLookupRefusesWhatItNeverMinted.
func TestSessionStoreLookupRefusesWhatItNeverMinted(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	id, err := store.create("local user", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, ok := store.lookup(id); !ok || got.subject != "local user" {
		t.Fatalf("a minted id did not look up: %v %v", got, ok)
	}
	for _, wrong := range []string{"", id[:len(id)-1], id + "0", flipLast(id), strings.ToUpper(id)} {
		if _, ok := store.lookup(wrong); ok {
			t.Errorf("%q looked up as a live session", wrong)
		}
	}
}

// flipLast changes an id by exactly one hex digit. Writing `id[:len(id)-1]+"0"`
// looks like the same thing and is not: one time in sixteen the id already
// ended in `0`, and the "wrong" id was the right one.
func flipLast(id string) string {
	last := "0"
	if strings.HasSuffix(id, "0") {
		last = "1"
	}
	return id[:len(id)-1] + last
}

/* The two doors -------------------------------------------------------------- */

// TestBearerHeaderTakesTheSecretAndNotASessionID: the two credentials are
// different things with different lifetimes, and one door does not open on the
// other's key.
func TestBearerHeaderTakesTheSecretAndNotASessionID(t *testing.T) {
	_, ts := newTestServer(t, false)
	cookie := launchSession(t, ts)

	resp := upgradeRequest(t, ts, "", ts.URL, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+cookie.Value)
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a session id in the Bearer header: status %d, want 401", resp.StatusCode)
	}

	// And the reverse: the launch secret in the cookie is not a session.
	refused := upgradeRequest(t, ts, "", ts.URL, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: testToken})
	})
	if refused.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the launch secret in the cookie: status %d, want 401", refused.StatusCode)
	}
}

// TestFileAPITakesTheCookieAndTheHeader covers the gated file API with each of
// the two doors, and with the query that is no longer one.
func TestFileAPITakesTheCookieAndTheHeader(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	cookie := launchSession(t, ts)

	withCookie, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files", nil)
	withCookie.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	resp, err := http.DefaultClient.Do(withCookie)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the cookie did not authorize the file API: status %d", resp.StatusCode)
	}

	if status, _ := getJSON(t, ts, "/api/files"); status != http.StatusOK {
		t.Fatalf("the Bearer header did not authorize the file API: status %d", status)
	}
}

// TestACookieFromAForeignOriginIsRefusedOnAWrite is the CSRF case on the one
// route that changes the project.
func TestACookieFromAForeignOriginIsRefusedOnAWrite(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"id":"a"}`)
	cookie := launchSession(t, ts)

	payload, _ := json.Marshal(WriteRequest{Path: "jpack.json", Content: "{}", Override: true})
	put, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file", strings.NewReader(string(payload)))
	put.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	put.Header.Set("Origin", "http://evil.example")
	put.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
	data, _ := os.ReadFile(filepath.Join(project, "jpack.json"))
	if string(data) != `{"id":"a"}` {
		t.Fatalf("a cross-origin write with a real cookie reached the disk: %q", data)
	}
}

/* GET /api/session ------------------------------------------------------------ */

func TestSessionEndpointAnswersTheCookiesSubject(t *testing.T) {
	_, ts := newTestServer(t, false)
	cookie := launchSession(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["subject"] != "local user" {
		t.Errorf("subject = %v, want %q", body["subject"], "local user")
	}
	// Null rather than absent and rather than empty: no provider authenticated
	// this session, and 7b is what fills it in.
	issuer, present := body["issuer"]
	if !present {
		t.Error("the answer carries no issuer member at all")
	}
	if issuer != nil {
		t.Errorf("issuer = %v, want null", issuer)
	}
	if len(body) != 2 {
		t.Errorf("the answer carries %d members, want exactly subject and issuer: %v", len(body), body)
	}
}

func TestSessionEndpointRefusesWithoutACookie(t *testing.T) {
	_, ts := newTestServer(t, false)

	// Nothing at all.
	anon, err := http.Get(ts.URL + "/api/session")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", anon.StatusCode)
	}

	// The launch secret gets past the guard and finds no session to describe:
	// a script presenting a secret is not a browser holding a session.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	bearer(req)
	scripted, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer scripted.Body.Close()
	if scripted.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a Bearer request: status %d, want 401", scripted.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(scripted.Body).Decode(&body)
	if body["code"] != CodeUnauthorized {
		t.Errorf("code %v, want %s", body["code"], CodeUnauthorized)
	}
}

func TestSessionEndpointRefusesAForeignOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	cookie := launchSession(t, ts)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
}

/* The removed door ------------------------------------------------------------ */

// TestTheQueryAuthorizesNothingAnywhere sweeps **every gated route** with the
// genuine launch secret on the query, under both spellings it ever had.
//
// One test per route would be one route away from a gap; this is the sweep, and
// a route added to the chassis without being added here is a route this suite
// does not speak for.
func TestTheQueryAuthorizesNothingAnywhere(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/files"},
		{http.MethodGet, "/api/file?path=jpack.json"},
		{http.MethodPut, "/api/file"},
		{http.MethodGet, "/api/session"},
		{http.MethodGet, "/api/desk-config"},
		{http.MethodPut, "/api/desk-config"},
		{http.MethodGet, "/api/assistant/key"},
		{http.MethodPut, "/api/assistant/key"},
		{http.MethodDelete, "/api/assistant/key"},
		{http.MethodPost, "/api/assistant/probe"},
		{http.MethodGet, relayPrefix + "models"},
		{http.MethodGet, "/ws"},
	}
	for _, route := range routes {
		for _, name := range []string{"token", "secret"} {
			separator := "?"
			if strings.Contains(route.path, "?") {
				separator = "&"
			}
			address := ts.URL + route.path + separator + name + "=" + testToken
			t.Run(route.method+" "+route.path+" ?"+name+"=", func(t *testing.T) {
				req, err := http.NewRequest(route.method, address, strings.NewReader("{}"))
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				resp, err := ts.Client().Do(req)
				if err != nil {
					t.Fatalf("do: %v", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("status %d, want 401", resp.StatusCode)
				}
			})
		}
	}
}

// TestTheLaunchPathIsTheOnlyPlaceASecretIsRead: `?secret=` is read at `/launch`
// and nowhere else, which is the other half of the sweep above.
func TestTheLaunchPathIsTheOnlyPlaceASecretIsRead(t *testing.T) {
	_, ts := newTestServer(t, false)
	// The static handler serves the SPA fallback, and a build without assets
	// answers 404 — either way, it does not mint a session.
	resp, err := ts.Client().Get(ts.URL + "/?secret=" + testToken)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName {
			t.Fatalf("a secret on the page's own URL minted a session: %v", cookie)
		}
	}
}

// TestLaunchAcceptsOnlyGET: an exchange is a read, and a `POST /launch` that
// minted a session would be one a form on another site could submit.
func TestLaunchAcceptsOnlyGET(t *testing.T) {
	_, ts := newTestServer(t, false)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(method, ts.URL+"/launch?secret="+testToken, nil)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		defer resp.Body.Close()
		for _, cookie := range resp.Cookies() {
			if cookie.Name == sessionCookieName {
				t.Fatalf("%s /launch minted a session", method)
			}
		}
	}
}
