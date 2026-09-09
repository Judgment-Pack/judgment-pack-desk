package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
)

/* The bootstrap --------------------------------------------------------------- */

// TestLaunchSetsAHandoffAndNoSession is the whole of the browser's entry: the
// printed URL in, a `303` to `/#` and **one short-lived, single-use cookie**
// out — and no session at all.
func TestLaunchSetsAHandoffAndNoSession(t *testing.T) {
	s, ts := newTestServer(t, false)
	resp := launchResponse(t, ts, testToken)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	// **`/#`, with the empty fragment written out.** A Location with no
	// fragment inherits the request's (RFC 9110 §10.2.2), so
	// `/launch?secret=S#S` would land on `/#S` with the secret in
	// `location.hash`. See `TestTheRedirectDropsTheRequestsFragment`.
	if got := resp.Header.Get("Location"); got != "/#" {
		t.Errorf("Location = %q, want %q", got, "/#")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	cookies := resp.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("the launch set %d cookies, want the handoff and its marker: %v", len(cookies), cookies)
	}
	cookie := launchHandoff(t, ts)

	// **The marker, and what it must not be.** It is readable by page code —
	// the only cookie on this desk that is — because the handoff is `HttpOnly`
	// and a page therefore cannot tell there is one to spend. It carries no
	// secret and expires with the thing it marks.
	var marker *http.Cookie
	for _, held := range cookies {
		if strings.HasPrefix(held.Name, "jpack-desk-handoff-pending-") {
			marker = held
		}
	}
	if marker == nil {
		t.Fatalf("the launch set no readable marker: %v", cookies)
	}
	if marker.HttpOnly {
		t.Error("the marker is HttpOnly, so the page cannot see there is a handoff to spend")
	}
	if marker.Value == "" || strings.Contains(marker.Value, cookie.Value) || marker.Value == testToken {
		t.Errorf("the marker carries something it should not: %q", marker.Value)
	}
	if marker.MaxAge != 60 {
		t.Errorf("the marker's Max-Age is %d, want the handoff's 60", marker.MaxAge)
	}

	// **The name carries the port**, because a cookie's origin does not: two
	// desks on one host would otherwise hand each other's tab the wrong handoff.
	want := fmt.Sprintf("jpack-desk-launch-%d", s.cfg.Port)
	if cookie.Name != want {
		t.Errorf("cookie name = %q, want %q", cookie.Name, want)
	}
	if cookie.Value == "" || cookie.Value == testToken {
		t.Fatalf("the handoff is %q — empty, or the launch secret itself", cookie.Value)
	}
	if len(cookie.Value) != 48 {
		t.Errorf("handoff is %d hex characters, want 48 (192 bits)", len(cookie.Value))
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Error("the handoff is not HttpOnly: page code can read it")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", cookie.SameSite)
	}
	// **Max-Age, and it is the difference between a window and a key.** A
	// handoff that outlived the page load it exists for would be a standing
	// credential in a cookie jar, which is the arrangement this replaced.
	if cookie.MaxAge != 60 {
		t.Errorf("Max-Age = %d, want 60", cookie.MaxAge)
	}
	// Not `Secure` on http: a browser discards a Secure cookie that arrives
	// over plain http, and this chassis serves plain http on loopback.
	if cookie.Secure {
		t.Error("Secure is set on an http response, so the browser would drop the cookie")
	}
	// And **no session was minted**. A launch that minted one would answer with
	// a standing credential the browser then attaches to everything.
	if n := s.sessions.count(); n != 0 {
		t.Fatalf("the launch minted %d session(s); it must mint none", n)
	}
	if strings.Contains(resp.Header.Get("Location"), testToken) {
		t.Error("the redirect carries the launch secret")
	}
}

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
				t.Fatalf("a refused launch set %d cookie(s): %v", len(cookies), cookies)
			}
		})
	}
	if n := s.launches.count(); n != 0 {
		t.Fatalf("%d handoff(s) were minted by refused launches", n)
	}
}

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
// second handoff and not a restart. Single-use *there* would make every closed
// tab a restart of the desk, for a property this design does not claim — what
// is single use is the handoff, which is the thing that is ambient.
func TestLaunchSecretStaysValidForTheProcess(t *testing.T) {
	_, ts := newTestServer(t, false)
	first := launchHandoff(t, ts)
	second := launchHandoff(t, ts)
	if first.Value == second.Value {
		t.Error("two launches produced one handoff")
	}
	for _, handoff := range []*http.Cookie{first, second} {
		acceptsSession(t, ts, exchange(t, ts, handoff), ts.URL)
	}
}

/* The exchange ---------------------------------------------------------------- */

// TestTheHandoffIsSingleUse — the property the residual rests on. A second
// `POST` with the same cookie is a `401`, so a stolen handoff makes the page
// fail visibly rather than letting two callers share one desk quietly.
func TestTheHandoffIsSingleUse(t *testing.T) {
	s, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)

	id := exchange(t, ts, handoff)
	if id == "" {
		t.Fatal("the first exchange answered no id")
	}
	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("the second exchange answered %d, want 401: %v", status, body)
	}
	if body["code"] != CodeUnauthorized {
		t.Errorf("code %v, want %s", body["code"], CodeUnauthorized)
	}
	if n := s.sessions.count(); n != 1 {
		t.Fatalf("%d sessions after one spent handoff, want 1", n)
	}
}

// TestTheExchangeClearsTheHandoff: spent, and said so on the wire, so the
// browser drops a value that is already worthless.
func TestTheExchangeClearsTheHandoff(t *testing.T) {
	_, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
	req.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
	req.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
	req.Header.Set("Origin", ts.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("the exchange set %v, want the handoff and its marker cleared", cookies)
	}
	for _, cleared := range cookies {
		if cleared.MaxAge >= 0 {
			t.Errorf("%s: Max-Age = %d, want a negative one that clears it", cleared.Name, cleared.MaxAge)
		}
		if cleared.Value != "" {
			t.Errorf("%s: the cleared cookie carries %q", cleared.Name, cleared.Value)
		}
		// **And it must not be `Secure` over plain http**, or it clears
		// nothing: a browser refuses a `Secure` cookie from a non-secure
		// origin, so the handoff and its marker would survive the request that
		// spent them and the page would try to spend a handoff that is gone on
		// every load. This desk is served over http on loopback.
		if cleared.Secure {
			t.Errorf("%s: Secure on a plain-http expiry, which a browser refuses", cleared.Name)
		}
	}
}

// TestTheHandoffExpires, with the clock injected rather than waited on.
func TestTheHandoffExpires(t *testing.T) {
	s, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)

	now := time.Now()
	s.launches.mu.Lock()
	s.launches.now = func() time.Time { return now.Add(launchWindow + time.Second) }
	s.launches.mu.Unlock()

	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("an expired handoff answered %d, want 401: %v", status, body)
	}
	if n := s.sessions.count(); n != 0 {
		t.Fatalf("an expired handoff minted %d session(s)", n)
	}
}

// TestTheExchangeRefusesWithoutASameOriginClaim. The handoff is ambient for its
// sixty seconds, and this is the only route it opens, so this is the one place
// a page that is not ours could drive somebody's cookie. `Sec-Fetch-Site` is
// belt-and-braces beside `SameSite=Strict`, and a browser cannot forge it.
func TestTheExchangeRefusesWithoutASameOriginClaim(t *testing.T) {
	_, ts := newTestServer(t, false)
	for _, claim := range []string{"", "same-site", "cross-site", "none", "Same-Origin"} {
		t.Run("Sec-Fetch-Site: "+claim, func(t *testing.T) {
			handoff := launchHandoff(t, ts)
			status, _ := exchangeAttempt(t, ts, func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
				if claim != "" {
					r.Header.Set(fetchSiteHeader, claim)
				}
				r.Header.Set("Origin", ts.URL)
			})
			if status != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", status)
			}
			// And the handoff is **not** spent by a refused attempt, so the
			// page that follows still works.
			if id := exchange(t, ts, handoff); id == "" {
				t.Fatal("a refused attempt spent the handoff")
			}
		})
	}
}

func TestTheExchangeRefusesAForeignOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)
	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", "http://evil.example")
	})
	if status != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %v", status, body)
	}
}

// TestAScriptMintsASessionWithTheLaunchSecret: no cookie, no fetch metadata,
// no browser.
func TestAScriptMintsASessionWithTheLaunchSecret(t *testing.T) {
	_, ts := newTestServer(t, false)
	status, body := exchangeAttempt(t, ts, bearer)
	if status != http.StatusOK {
		t.Fatalf("status %d, want 200: %v", status, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("no id: %v", body)
	}
	acceptsSession(t, ts, id, "")
}

// TestTheStatedResidual is the one this design does **not** claim to prevent,
// written down as a test so that it is a known property rather than a surprise.
//
// A script that captures the handoff inside its window and forges
// `Sec-Fetch-Site: same-origin` takes the session first. Forbidden-header rules
// bind browsers, not scripts. What the shape buys is that the theft is
// **visible**: the handoff is single use, so the page's own exchange then fails
// and the desk says it has no session rather than working while somebody else
// is also inside.
func TestTheStatedResidual(t *testing.T) {
	_, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)

	// The thief, inside the window, forging the one header a browser would not
	// let a page write.
	stolen, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if stolen != http.StatusOK {
		t.Fatalf("the residual is not what this test says it is: status %d", stolen)
	}
	if id, _ := body["id"].(string); id == "" {
		t.Fatal("the thief got no id")
	}

	// And the page's own exchange then fails, which is the bound. What the
	// person sees depends on whether that tab already held a session — see
	// `createSession`'s comment and the web suite's "keeps the old id where the
	// exchange is refused"; this asserts the wire, which is the half that lives
	// here.
	after, _ := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if after != http.StatusUnauthorized {
		t.Fatalf("the page's own exchange answered %d after a theft, want 401", after)
	}
}

/* Nothing ambient authorizes anything ------------------------------------------ */

// TestNoRouteEverSetsASessionCookie sweeps every route this chassis has and
// asserts that **no answer carries a live cookie** except the launch's handoff
// and its marker.
//
// **Each route gets its own session, and each is required to succeed.** The
// first version shared one id across the sweep and included `DELETE
// /api/session`, so every route after it was answering `401` — and a `401` sets
// no cookies, so the assertion passed for the wrong reason on half the table.
// Asserting the success status is what makes "this handler set no cookie" a
// statement about the handler rather than about the guard in front of it.
func TestNoRouteEverSetsASessionCookie(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")

	type attempt struct {
		name string
		want int
		do   func(t *testing.T, id string) *http.Response
	}
	plain := func(method, path string, want int) attempt {
		return attempt{method + " " + path, want, func(t *testing.T, id string) *http.Response {
			t.Helper()
			req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(`{"key":"placeholder-never-sent-000"}`))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			pageBearer(id)(req)
			req.Header.Set("Content-Type", "application/json")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}}
	}
	for _, route := range []attempt{
		plain(http.MethodGet, "/api/files", http.StatusOK),
		plain(http.MethodGet, "/api/file?path=jpack.json", http.StatusOK),
		plain(http.MethodGet, "/api/session", http.StatusOK),
		plain(http.MethodGet, "/api/desk-config", http.StatusOK),
		plain(http.MethodGet, "/api/assistant/key", http.StatusOK),
		// A `409`, and it is still the handler answering: storing a key needs a
		// configured endpoint to bind it to, and this desk has none. What this
		// row asserts is that the *guard* was passed and the handler ran.
		plain(http.MethodPut, "/api/assistant/key", http.StatusConflict),
		plain(http.MethodDelete, "/api/assistant/key", http.StatusOK),
		plain(http.MethodDelete, "/api/session", http.StatusOK),
		plain(http.MethodGet, "/", http.StatusNotFound),
		plain(http.MethodGet, "/packs/anything", http.StatusNotFound),
		{"POST /api/session", http.StatusOK, func(t *testing.T, _ string) *http.Response {
			t.Helper()
			// The exchange has its own shape: a handoff and the same-origin
			// claim, which is the only route on this desk that takes either.
			handoff := launchHandoff(t, ts)
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
			req.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
			req.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
			req.Header.Set("Origin", ts.URL)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		{"GET /ws", http.StatusSwitchingProtocols, func(t *testing.T, id string) *http.Response {
			t.Helper()
			return upgradeRequest(t, ts, "", ts.URL, func(r *http.Request) {
				r.Header.Set(wsProtocolHeader, strings.Join(upgradeOffer(id), ", "))
			})
		}},
	} {
		t.Run(route.name, func(t *testing.T) {
			// **A session of its own**, so that one route's sign-out cannot
			// make the next route's assertion vacuous.
			id := beginSession(t, ts)
			resp := route.do(t, id)
			defer resp.Body.Close()
			if resp.StatusCode != route.want {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status %d, want %d — the handler was not reached: %s",
					resp.StatusCode, route.want, body)
			}
			for _, cookie := range resp.Cookies() {
				if cookie.Value != "" && cookie.MaxAge >= 0 {
					t.Fatalf("%s set a live cookie: %v", route.name, cookie)
				}
			}
		})
	}
}

// TestNoCookieAuthorizesAnyGatedRoute. Every cookie shape, on every gated
// route: none of them is an authorization, whatever else is on the request.
//
// **This is the round-2 finding, held.** A cookie replayed by a script with a
// forged `Sec-Fetch-Site` used to read this desk; there is nothing to replay
// now, because no cookie is consulted anywhere but the exchange.
func TestNoCookieAuthorizesAnyGatedRoute(t *testing.T) {
	s, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	handoff := launchHandoff(t, ts)
	id := beginSession(t, ts)

	cookies := []*http.Cookie{
		{Name: handoff.Name, Value: handoff.Value},
		{Name: s.launchCookie, Value: id},
		{Name: fmt.Sprintf("jpack-desk-session-%d", s.cfg.Port), Value: id},
		{Name: "jpack-desk-session", Value: id},
	}
	routes := []string{"/api/files", "/api/file?path=jpack.json", "/api/session", "/api/desk-config"}
	for _, route := range routes {
		for _, cookie := range cookies {
			for _, claim := range []string{"", fetchSiteSameOrigin, "same-site"} {
				name := route + " " + cookie.Name + " " + claim
				t.Run(name, func(t *testing.T) {
					req, _ := http.NewRequest(http.MethodGet, ts.URL+route, nil)
					req.AddCookie(cookie)
					if claim != "" {
						req.Header.Set(fetchSiteHeader, claim)
					}
					req.Header.Set("Origin", ts.URL)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatalf("get: %v", err)
					}
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusUnauthorized {
						t.Fatalf("status %d, want 401", resp.StatusCode)
					}
				})
			}
		}
	}
}

// TestTheBearerAuthorizesEveryGatedRoute is the positive control for the sweep
// above: the same routes, the same origin, the id on the header instead.
func TestTheBearerAuthorizesEveryGatedRoute(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	id := beginSession(t, ts)

	for _, route := range []string{"/api/files", "/api/file?path=jpack.json", "/api/session", "/api/desk-config"} {
		t.Run(route, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, ts.URL+route, nil)
			pageBearer(id)(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d, want 200", resp.StatusCode)
			}
		})
	}
}

// TestTheQueryAuthorizesNothingAnywhere sweeps every gated route with the
// genuine launch secret **and** a live session id on the query, under every
// spelling either has ever had.
func TestTheQueryAuthorizesNothingAnywhere(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	id := beginSession(t, ts)

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
		for _, pair := range []struct{ name, value string }{
			{"token", testToken}, {"secret", testToken},
			{"token", id}, {"session", id},
		} {
			separator := "?"
			if strings.Contains(route.path, "?") {
				separator = "&"
			}
			address := ts.URL + route.path + separator + pair.name + "=" + pair.value
			t.Run(route.method+" "+route.path+" ?"+pair.name, func(t *testing.T) {
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

// TestBearerTakesTheSecretOrASessionAndNothingElse.
func TestBearerTakesTheSecretOrASessionAndNothingElse(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	handoff := launchHandoff(t, ts)

	// The two that work.
	acceptsSession(t, ts, id, ts.URL)
	if resp := upgradeRequest(t, ts, "", ts.URL, bearer); resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("the launch secret does not authorize")
	}
	// And a handoff is not one of them: it opens the exchange and nothing else.
	refused := upgradeRequest(t, ts, "", ts.URL, pageBearer(handoff.Value))
	if refused.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a handoff opened the relay: status %d", refused.StatusCode)
	}
}

/* Two desks, one browser -------------------------------------------------------- */

// TestTwoDesksDoNotShareASession. Cookies are keyed by host, so a browser that
// has met both desks sends both handoffs to both; the names keep them apart,
// and the sessions they buy are bearers the page keys by port and never sends
// anywhere but the desk it came from.
func TestTwoDesksDoNotShareASession(t *testing.T) {
	_, first := newTestServer(t, false)
	_, second := newTestServer(t, false)
	if first.URL == second.URL {
		t.Fatal("the two desks are on one port")
	}

	oneHandoff := launchHandoff(t, first)
	twoHandoff := launchHandoff(t, second)
	if oneHandoff.Name == twoHandoff.Name {
		t.Fatalf("both desks named their handoff %q", oneHandoff.Name)
	}

	// The jar, holding both, exactly as a browser that has opened both would.
	// Each desk must read its own by name, and spend only that one.
	jar := []*http.Cookie{
		{Name: oneHandoff.Name, Value: oneHandoff.Value},
		{Name: twoHandoff.Name, Value: twoHandoff.Value},
	}
	oneID := exchangeWithJar(t, first, jar)
	twoID := exchangeWithJar(t, second, jar)
	if oneID == twoID {
		t.Fatal("both desks answered the same session id")
	}

	// And neither desk's id is a session on the other: the store's MAC key is
	// per process, and the page keys its storage by port so it never offers
	// one to the other in the first place.
	acceptsSession(t, first, oneID, first.URL)
	acceptsSession(t, second, twoID, second.URL)
	if resp := upgradeRequest(t, first, "", first.URL, pageBearer(twoID)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the second desk's id opened the first: status %d", resp.StatusCode)
	}
	if resp := upgradeRequest(t, second, "", second.URL, pageBearer(oneID)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the first desk's id opened the second: status %d", resp.StatusCode)
	}
}

func exchangeWithJar(t *testing.T, ts *httptest.Server, jar []*http.Cookie) string {
	t.Helper()
	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		for _, cookie := range jar {
			r.AddCookie(cookie)
		}
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusOK {
		t.Fatalf("exchange status %d: %v", status, body)
	}
	id, _ := body["id"].(string)
	return id
}

/* The redirect, and what is under /launch --------------------------------------- */

func TestTheRedirectDropsTheRequestsFragment(t *testing.T) {
	_, ts := newTestServer(t, false)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(ts.URL + "/launch?secret=" + testToken)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer resp.Body.Close()
	location := resp.Header.Get("Location")
	if location != "/#" {
		t.Fatalf("Location = %q, want %q — a bare `/` inherits the request's fragment", location, "/#")
	}
	if strings.Contains(location, testToken) {
		t.Error("the redirect carries the launch secret")
	}
}

// thePage is the marker the stand-in single-page shell carries, so a test can
// tell "the fallback served the page" from "there was nothing to serve".
const thePage = "THE SINGLE-PAGE SHELL"

// deskWithAPage is a chassis that actually serves one, because `newTestServer`
// has no assets and answers 404 for a client-side route and a missing file
// alike.
func deskWithAPage(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s, ts := startDesk(t, Config{
		ProjectDir: t.TempDir(),
		JpackBin:   "jpack",
		Token:      testToken,
		Static:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(thePage)}},
		Logger:     log.New(io.Discard, "", 0),
	})
	t.Cleanup(func() {
		ts.Close()
		_ = s.Close()
	})
	return s, ts
}

// TestNothingThatLooksLikeALaunchIsAnsweredWithThePage.
//
// `GET /launch` matches one exact path, so `/launch/anything?secret=…` used to
// reach the SPA fallback and be answered with the page — the secret then sitting
// in `window.location`, in history, and in every `Referer` that page sends. The
// router owns `/launch` and `/launch/…`; the static handler owns the spellings
// it does not see, which is why another case and a stray `?secret=` are here.
func TestNothingThatLooksLikeALaunchIsAnsweredWithThePage(t *testing.T) {
	_, ts := deskWithAPage(t)

	// **The positive control, first.** Without it this test passes on a desk
	// that serves no page at all, which is every other server in this package.
	served, err := ts.Client().Get(ts.URL + "/packs/anything")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer served.Body.Close()
	shell, _ := io.ReadAll(served.Body)
	if served.StatusCode != http.StatusOK || !strings.Contains(string(shell), thePage) {
		t.Fatalf("the single-page fallback is not live: %d %q", served.StatusCode, shell)
	}

	for _, path := range []string{
		"/launch/",
		"/launch/anything",
		"/launch/anything?secret=" + testToken,
		"/launch/a/b/c",
		"/Launch",
		"/LAUNCH/anything",
		"/Launch/anything?secret=" + testToken,
		"/launch%2Fanything",
		"/packs/x?secret=" + testToken,
		"/?secret=" + testToken,
		"/anything?a=1&secret=" + testToken,
		// Case-folded on the name: `?SECRET=` is a different parameter to
		// `url.Query`, and "the page is harmless so the spelling does not
		// matter" is the reasoning that put a secret in an address bar twice.
		"/anything?SECRET=" + testToken,
		"/anything?Secret=" + testToken,
	} {
		t.Run(path, func(t *testing.T) {
			client := &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
			resp, err := client.Get(ts.URL + path)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			for _, cookie := range resp.Cookies() {
				if cookie.Value != "" {
					t.Fatalf("%s set %v", path, cookie)
				}
			}
			if strings.Contains(string(body), thePage) {
				t.Fatalf("%s was answered with the page (%d)", path, resp.StatusCode)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s answered %d, want 404", path, resp.StatusCode)
			}
			if !strings.Contains(string(body), "nothing under /launch") {
				t.Fatalf("%s answered %q, want the launch refusal", path, body)
			}
		})
	}
}

/* The stores -------------------------------------------------------------------- */

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
		if len(key) != 64 {
			t.Fatalf("key %q is %d characters, want a 64-character MAC", key, len(key))
		}
	}
}

func TestSessionHandleIsNotTheID(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	const id = "0123456789abcdef0123456789abcdef0123456789abcdef"
	handle := store.handle(id)
	if handle == id || strings.Contains(handle, id) {
		t.Fatal("the handle reproduces the id")
	}
	if store.handle(id) != handle {
		t.Fatal("the handle is not stable for one id")
	}
	if store.handle(flipLast(id)) == handle {
		t.Fatal("two different ids share a handle")
	}
	other, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	if other.handle(id) == handle {
		t.Fatal("two stores derive the same handle, so the MAC key is not per process")
	}
}

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

// TestTheSessionStoreIsBoundedAndLRU. The bound stops a desk left open all day
// growing a map entry per reopened tab; **least recently used** rather than
// oldest is what keeps the tab somebody is actually looking at.
func TestTheSessionStoreIsBoundedAndLRU(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	first, err := store.create("local user", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ids := []string{first}
	for range maxSessions - 1 {
		id, err := store.create("local user", nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, id)
		if n := store.count(); n > maxSessions {
			t.Fatalf("the store holds %d sessions, past the bound of %d", n, maxSessions)
		}
	}
	if n := store.count(); n != maxSessions {
		t.Fatalf("the store holds %d, want it full at %d", n, maxSessions)
	}

	// **The oldest session, used.** Under oldest-first it would be the next to
	// go; under LRU it is now the newest thing in the store.
	if _, ok := store.lookup(first); !ok {
		t.Fatal("the first session is gone before anything was evicted")
	}
	for range 8 {
		if _, err := store.create("local user", nil); err != nil {
			t.Fatalf("create: %v", err)
		}
		if n := store.count(); n > maxSessions {
			t.Fatalf("the store holds %d, past the bound", n)
		}
	}
	if _, ok := store.lookup(first); !ok {
		t.Fatal("a session that was used most recently was evicted: the bound is not LRU")
	}
	// And the ones nobody touched went, oldest of those first.
	if _, ok := store.lookup(ids[1]); ok {
		t.Error("an untouched session survived eight evictions")
	}
}

func TestTheBoundIsEnforcedThroughTheExchange(t *testing.T) {
	s, ts := newTestServer(t, false)
	newest := ""
	for range maxSessions + 8 {
		newest = beginSession(t, ts)
	}
	if n := s.sessions.count(); n != maxSessions {
		t.Fatalf("%d live sessions after %d exchanges, want the bound of %d",
			n, maxSessions+8, maxSessions)
	}
	acceptsSession(t, ts, newest, ts.URL)
}

func TestTheLaunchStoreIsBoundedAndSwept(t *testing.T) {
	store, err := newLaunchStore()
	if err != nil {
		t.Fatalf("newLaunchStore: %v", err)
	}
	for range maxLaunches * 2 {
		if _, err := store.issue(); err != nil {
			t.Fatalf("issue: %v", err)
		}
		if n := store.count(); n > maxLaunches {
			t.Fatalf("%d handoffs, past the bound of %d", n, maxLaunches)
		}
	}
	// And expiry sweeps rather than accumulating.
	now := time.Now()
	store.mu.Lock()
	store.now = func() time.Time { return now.Add(launchWindow + time.Second) }
	store.mu.Unlock()
	if _, err := store.issue(); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if n := store.count(); n != 1 {
		t.Fatalf("%d handoffs after a sweep, want only the new one", n)
	}
}

/* GET and DELETE /api/session ---------------------------------------------------- */

func TestSessionEndpointAnswersTheBearersSubject(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	pageBearer(id)(req)
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
	issuer, present := body["issuer"]
	if !present {
		t.Error("the answer carries no issuer member at all")
	}
	if issuer != nil {
		t.Errorf("issuer = %v, want null", issuer)
	}
	if len(body) != 2 {
		t.Errorf("the answer carries %d members, want subject and issuer: %v", len(body), body)
	}
}

func TestSessionEndpointRefusesWithoutABearer(t *testing.T) {
	_, ts := newTestServer(t, false)
	anon, err := http.Get(ts.URL + "/api/session")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", anon.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(anon.Body).Decode(&body)
	if body["code"] != CodeUnauthorized {
		t.Errorf("code %v, want %s", body["code"], CodeUnauthorized)
	}
}

func TestSessionEndpointRefusesAForeignOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	pageBearer(id)(req)
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

// TestSignOutForgetsTheSession. Sign-out exists; expiry still does not, and the
// README says so rather than letting this route imply otherwise.
func TestSignOutForgetsTheSession(t *testing.T) {
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)
	if n := s.sessions.count(); n != 1 {
		t.Fatalf("%d sessions before sign-out", n)
	}

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
	pageBearer(id)(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["forgotten"] != true {
		t.Errorf("forgotten = %v, want true", body["forgotten"])
	}
	if n := s.sessions.count(); n != 0 {
		t.Fatalf("%d sessions after sign-out", n)
	}
	// And the id names nothing now.
	if resp := upgradeRequest(t, ts, "", ts.URL, pageBearer(id)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a signed-out id still opens the relay: status %d", resp.StatusCode)
	}
}

/* The file API, and the write nobody may make ------------------------------------ */

func TestABearerFromAForeignOriginIsRefusedOnAWrite(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"id":"a"}`)
	id := beginSession(t, ts)

	payload, _ := json.Marshal(WriteRequest{Path: "jpack.json", Content: "{}", Override: true})
	put, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file", strings.NewReader(string(payload)))
	pageBearer(id)(put)
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
		t.Fatalf("a cross-origin write with a real bearer reached the disk: %q", data)
	}
}

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
			if cookie.Value != "" {
				t.Fatalf("%s /launch set %v", method, cookie)
			}
		}
	}
}

/* The relaunch, the raw query, the sockets, and recency ------------------------ */

// TestARelaunchIsAlwaysSpendable is the chassis half of the relaunch fix: a
// launch into a tab that already has a session sets a handoff **and a readable
// marker**, and the old session keeps working until the new one lands.
//
// The page's half — spending it — is `web/src/mcp/session.test.tsx`. What this
// pins is that the chassis gives the page something to act on, and that a
// relaunch does not disturb what is already working.
func TestARelaunchIsAlwaysSpendable(t *testing.T) {
	_, ts := newTestServer(t, false)
	first := beginSession(t, ts)

	resp := launchResponse(t, ts, testToken)
	defer resp.Body.Close()
	var marker, handoff *http.Cookie
	for _, cookie := range resp.Cookies() {
		if strings.HasPrefix(cookie.Name, "jpack-desk-handoff-pending-") {
			marker = cookie
		}
		if strings.HasPrefix(cookie.Name, launchCookiePrefix+"-") {
			handoff = cookie
		}
	}
	if marker == nil || handoff == nil {
		t.Fatalf("a relaunch set %v, want a handoff and a marker", resp.Cookies())
	}
	if marker.HttpOnly {
		t.Fatal("the marker is HttpOnly, so a page holding an id cannot tell there is a handoff waiting")
	}

	// The old session still works — the page has not replaced it yet, and a
	// relaunch that broke the tab it was opened from would be a poor trade.
	acceptsSession(t, ts, first, ts.URL)

	// And the new handoff is spendable, which is what the page does next.
	second := exchange(t, ts, handoff)
	if second == first {
		t.Fatal("the relaunch answered the same session id")
	}
	acceptsSession(t, ts, second, ts.URL)
}

// TestTheSecretRuleReadsTheRawQuery.
//
// `url.Query()` is a parser, and a parser has opinions: it drops a pair it
// cannot decode and it does not split on `;`. So `?secret=<real>%ZZ` parsed to
// nothing and `?x=1;secret=<real>` parsed to one parameter named `x`, and both
// were answered **with the page** — the secret then sitting in
// `window.location`, in history, and in every `Referer` that page sent.
func TestTheSecretRuleReadsTheRawQuery(t *testing.T) {
	_, ts := deskWithAPage(t)
	for _, raw := range []string{
		"secret=" + testToken,
		"SECRET=" + testToken,
		"Secret=" + testToken,
		"%73ecret=" + testToken,
		"%53ECRET=" + testToken,
		"x=1;secret=" + testToken,
		"secret=" + testToken + ";x=1",
		"secret=" + testToken + "%ZZ",
		"a=%ZZ&secret=" + testToken,
		"a=%ZZ&%73ecret=" + testToken,
		// **The combined case**, which is the one that got through: an encoded
		// name *and* an invalid escape in the same query. `url.QueryUnescape`
		// is all-or-nothing, so the bad byte bought silence about the whole
		// string and the page was served.
		"%73ecret=" + testToken + "%ZZ",
		"%53ECRET=" + testToken + "%ZZ",
		"%53ECRET%ZZ=" + testToken,
		"a=1;%73ecret=" + testToken + "%ZZ",
		"secret",
		"mysecret=1",
		"a=secret",
	} {
		t.Run(raw, func(t *testing.T) {
			resp, err := ts.Client().Get(ts.URL + "/packs/x?" + raw)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), thePage) {
				t.Fatalf("?%s was answered with the page", raw)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("?%s answered %d, want 404", raw, resp.StatusCode)
			}
		})
	}

	// **A query that mentions no secret is served**, which is the control: a
	// rule that refused everything would pass every row above.
	for _, raw := range []string{"", "edit=1", "path=a.json&x=2", "a=%ZZ"} {
		t.Run("served/"+raw, func(t *testing.T) {
			resp, err := ts.Client().Get(ts.URL + "/packs/x?" + raw)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), thePage) {
				t.Fatalf("?%s was refused (%d): %s", raw, resp.StatusCode, body)
			}
		})
	}
}

// TestTheRuleIsAboutTheQueryAndNotTheFragment. A fragment is never sent to a
// server, so there is nothing on the wire to refuse; what stops one reaching
// the page's address is the launch redirect's explicit empty fragment, which
// `TestTheRedirectDropsTheRequestsFragment` covers.
func TestTheRuleIsAboutTheQueryAndNotTheFragment(t *testing.T) {
	if querySmellsOfASecret("") {
		t.Error("an empty query smells of a secret")
	}
	if !querySmellsOfASecret("secret=x") {
		t.Error("a plain secret pair does not")
	}
	// The fragment never arrives, so this function never sees one. Written down
	// so that a reader does not add a rule for a case that cannot occur.
	if querySmellsOfASecret("edit=1") {
		t.Error("an ordinary query smells of a secret")
	}
}

// TestSignOutClosesTheSessionsSockets. A relay socket outlives the request that
// opened it, so a sign-out that only emptied the store would end the session
// everywhere except where it was actually being used.
func TestSignOutClosesTheSessionsSockets(t *testing.T) {
	if !runtimeAvailable() {
		t.Skip("no runtime binary: this drives a real relay socket")
	}
	_, ts := newTestServer(t, false)
	mine := beginSession(t, ts)
	other := beginSession(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func(id string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
			HTTPHeader:   http.Header{"Origin": []string{ts.URL}},
			Subprotocols: upgradeOffer(id),
		})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		c.SetReadLimit(readLimit)
		(&rpcSession{t: t, ctx: ctx, ws: c}).initialize()
		return c
	}
	ending := open(mine)
	defer ending.Close(websocket.StatusNormalClosure, "")
	surviving := open(other)
	defer surviving.Close(websocket.StatusNormalClosure, "")

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
	pageBearer(mine)(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign-out status %d", resp.StatusCode)
	}

	// **The close status, not merely an error.** The first version of this
	// looped until any read failed, and a healthy socket fails a read the
	// moment its deadline expires — so it passed whether or not the socket had
	// been closed. What discriminates is *why* it ended: sign-out closes with
	// `StatusPolicyViolation` and a reason, and a socket nobody closed produces
	// a context error with no close status at all.
	deadline, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	_, _, err = ending.Read(deadline)
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("the signed-out socket ended with close status %v (%v), want %v",
			got, err, websocket.StatusPolicyViolation)
	}

	// And the other session's socket is untouched.
	(&rpcSession{t: t, ctx: ctx, ws: surviving}).call(4, "list_packs", map[string]any{})
}

// TestTrafficRefreshesRecency. A tab that has been driving the runtime for an
// hour has not been "looked up" since its bootstrap, so under a
// least-recently-used bound it was the coldest thing in the store and the first
// to go — the busiest desk being the one that stopped working.
func TestTrafficRefreshesRecency(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	busy, err := store.create("local user", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := store.handle(busy)
	for range maxSessions * 2 {
		if _, err := store.create("local user", nil); err != nil {
			t.Fatalf("create: %v", err)
		}
		// One frame's worth of traffic, which is what the relay does per second
		// on an open socket.
		store.touch(handle)
	}
	if _, ok := store.lookup(busy); !ok {
		t.Fatal("a session with traffic on it was evicted: the relay's touch is not reaching the store")
	}
}

// TestAnOpenSocketKeepsItsSessionAlive is the relay's *call site*, which the
// store-level test cannot reach: `TestTrafficRefreshesRecency` calls `touch`
// directly, so a relay that stopped calling it would leave that test green.
//
// Here a socket is opened, sixty-five sessions are minted underneath it, and
// frames keep arriving on it throughout. Under a least-recently-used bound the
// busiest tab would otherwise be the coldest thing in the store and the first
// to go — the desk somebody is using being the one that stops working.
func TestAnOpenSocketKeepsItsSessionAlive(t *testing.T) {
	if !runtimeAvailable() {
		t.Skip("no runtime binary: this drives a real relay socket")
	}
	s, ts := newTestServer(t, false)
	busy := beginSession(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{ts.URL}},
		Subprotocols: upgradeOffer(busy),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(readLimit)
	driven := &rpcSession{t: t, ctx: ctx, ws: c}
	driven.initialize()

	// **The store filled to just under the bound**, so that the next creates are
	// the ones that evict. `busy` was minted first and is the coldest thing in
	// it — under oldest-first it goes now, and under LRU it goes unless
	// something says it is in use.
	for range maxSessions - 4 {
		if _, err := s.sessions.create("local user", nil); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// **One second, and one call.** The relay refreshes at most once a second,
	// so a call inside the same second as `initialize` would touch nothing and
	// this test would pass on the initialize alone. The sleep is what makes the
	// call below the thing under test.
	time.Sleep(1100 * time.Millisecond)
	driven.call(100, "list_packs", map[string]any{})

	// And now past the bound, several times over.
	for range 8 {
		if _, err := s.sessions.create("local user", nil); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	if _, ok := s.sessions.lookup(busy); !ok {
		t.Fatal("a session with a socket driving traffic on it was evicted")
	}
}

// TestEvictionEndsTheSessionsSockets: the same close a sign-out performs, on
// the path nobody asked for.
func TestEvictionEndsTheSessionsSockets(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	ended := make(chan string, maxSessions*2)
	store.whenEnded(func(handle string) { ended <- handle })

	first, err := store.create("local user", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := store.handle(first)
	for range maxSessions {
		if _, err := store.create("local user", nil); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	close(ended)
	for handle := range ended {
		if handle == want {
			return
		}
	}
	t.Fatal("an evicted session's sockets were never ended")
}

/* Round 4: the siblings each fix missed --------------------------------------- */

// TestALenientDecodeSeesPastOneBadEscape is item 1 as a unit, beside the
// end-to-end rows above: `url.QueryUnescape` returns nothing at all when one
// escape anywhere is invalid, and that silence was the hole.
func TestALenientDecodeSeesPastOneBadEscape(t *testing.T) {
	for _, raw := range []string{
		"%73ecret=x%ZZ",
		"%53ECRET%ZZ=x",
		"a=%ZZ&%73ecret=x",
		"%73%65%63%72%65%74=x",
		"x=%ZZ%73ecret",
	} {
		if !querySmellsOfASecret(raw) {
			t.Errorf("%q was not seen as mentioning a secret", raw)
		}
	}
	// One decode pass, because that is how many the server does. A parameter
	// literally named `%73ecret` is not a secret arriving, and reading it as one
	// would be this rule disagreeing with the router about what the URL says.
	for _, raw := range []string{
		"%25%37%33ecret=x",
	} {
		if querySmellsOfASecret(raw) {
			t.Errorf("%q was read through a second decode pass", raw)
		}
	}
	// And `+` is left alone rather than read as a space: this is not a form
	// decoder, and a rule that invented characters would be one more reader to
	// disagree with.
	for _, raw := range []string{"", "edit=1", "a=%ZZ", "a=b+c", "%41=1"} {
		if querySmellsOfASecret(raw) {
			t.Errorf("%q was seen as mentioning a secret", raw)
		}
	}
}

// TestOnlyTheOfferCarriesASessionOntoTheUpgrade is item 2. A socket authorized
// by `Authorization: Bearer <session id>` carried no session **handle**, so
// sign-out and eviction could not find it: it kept driving the runtime for a
// session that had ended. There is one credential each way on this route now.
func TestOnlyTheOfferCarriesASessionOntoTheUpgrade(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	// The header path takes the launch secret and nothing else.
	refused := upgradeRequest(t, ts, "", ts.URL, pageBearer(id))
	if refused.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a session id on the upgrade's Authorization header: status %d, want 401",
			refused.StatusCode)
	}
	if resp := upgradeRequest(t, ts, "", ts.URL, bearer); resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("the launch secret does not open the relay")
	}
	// And the offer does.
	acceptsSession(t, ts, id, ts.URL)
}

// TestSignOutClosesOnlyTheSessionsOwnSockets. One socket per credential path:
// the page's, bound to the session, and a script's, bound to none.
func TestSignOutClosesOnlyTheSessionsOwnSockets(t *testing.T) {
	if !runtimeAvailable() {
		t.Skip("no runtime binary: this drives real relay sockets")
	}
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	dial := func(options *websocket.DialOptions) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", options)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		c.SetReadLimit(readLimit)
		(&rpcSession{t: t, ctx: ctx, ws: c}).initialize()
		return c
	}
	page := dial(&websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{ts.URL}},
		Subprotocols: upgradeOffer(id),
	})
	defer page.Close(websocket.StatusNormalClosure, "")
	script := dial(&websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + testToken}},
	})
	defer script.Close(websocket.StatusNormalClosure, "")

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
	pageBearer(id)(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign-out status %d", resp.StatusCode)
	}

	deadline, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	_, _, readErr := page.Read(deadline)
	if got := websocket.CloseStatus(readErr); got != websocket.StatusPolicyViolation {
		t.Fatalf("the page's socket ended with %v (%v), want %v",
			got, readErr, websocket.StatusPolicyViolation)
	}
	// **A script's socket is bound to no session and is untouched.** It never
	// was one, so signing one out cannot end it.
	(&rpcSession{t: t, ctx: ctx, ws: script}).call(7, "list_packs", map[string]any{})
}

// TestASignOutBetweenAuthorizationAndRegistration is item 3: the window between
// the gate saying yes and the connection being registered. A sign-out in it used
// to walk the connections, find nothing, and leave a socket driving the runtime
// for a session that had ended.
func TestASignOutBetweenAuthorizationAndRegistration(t *testing.T) {
	if !runtimeAvailable() {
		t.Skip("no runtime binary: this drives a real relay socket")
	}
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	// The seam: the upgrade pauses here, the session is signed out, and the
	// upgrade then continues into a store that no longer has it.
	signedOut := make(chan struct{})
	s.beforeRegister = func() {
		s.beforeRegister = nil
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
		pageBearer(id)(req)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		close(signedOut)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{ts.URL}},
		Subprotocols: upgradeOffer(id),
	})
	if err != nil {
		// The handshake itself may lose the race; either way nothing is serving.
		<-signedOut
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	<-signedOut

	deadline, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	_, _, readErr := c.Read(deadline)
	if readErr == nil {
		t.Fatal("a socket registered after its session was signed out is serving")
	}
	if got := websocket.CloseStatus(readErr); got != websocket.StatusPolicyViolation &&
		got != websocket.StatusInternalError && got != -1 {
		t.Logf("the socket ended with close status %v (%v)", got, readErr)
	}
	if n := s.sessions.count(); n != 0 {
		t.Fatalf("%d sessions after the sign-out", n)
	}
}

// recencyOf reads the store's own ordering number for a session, which is what
// eviction sorts on. A test that asserts "this was touched" against the number
// eviction actually reads cannot pass on a touch that went somewhere else.
func recencyOf(t *testing.T, store *sessionStore, id string) uint64 {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	held, ok := store.live[store.handle(id)]
	if !ok {
		t.Fatalf("the session is not in the store")
	}
	return held.used
}

// waitForATouch waits for the relay to refresh a session, and says so when it
// does not. **This is the assertion**: the touch has to arrive through the call
// site under test, so a row that deletes that call site fails here rather than
// somewhere a second call site would cover.
func waitForATouch(t *testing.T, store *sessionStore, id string, was uint64, what string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if recencyOf(t, store, id) != was {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s did not refresh the session's recency", what)
}

// TestInboundFramesRefreshTheSession and TestOutboundFramesRefreshTheSession are
// item 7, **one direction each**.
//
// The first version of this pair could not tell them apart. Every JSON-RPC call
// is a frame each way, so a socket driven by `call` touches through whichever
// site is left and both rows reported NOT DISCRIMINATING — the same "one path,
// not its siblings" mistake the round was about, made in the test rather than
// the code. So each test now drives **one** direction: a notification, which the
// runtime answers with nothing, and a desk-side broadcast, which the page never
// asked for.
func TestInboundFramesRefreshTheSession(t *testing.T) {
	if !runtimeAvailable() {
		t.Skip("no runtime binary: this drives a real relay socket")
	}
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{ts.URL}},
		Subprotocols: upgradeOffer(id),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(readLimit)
	driven := &rpcSession{t: t, ctx: ctx, ws: c}
	driven.initialize()

	// Past the rate limit, so the frame below is the thing being measured and
	// not the initialize a second ago.
	time.Sleep(1100 * time.Millisecond)
	was := recencyOf(t, s.sessions, id)
	// **A notification, so nothing comes back.** The outbound site cannot be
	// what refreshes this.
	driven.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	waitForATouch(t, s.sessions, id, was, "an inbound frame")
}

func TestOutboundFramesRefreshTheSession(t *testing.T) {
	if !runtimeAvailable() {
		t.Skip("no runtime binary: this drives a real relay socket")
	}
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{ts.URL}},
		Subprotocols: upgradeOffer(id),
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(readLimit)
	driven := &rpcSession{t: t, ctx: ctx, ws: c}
	driven.initialize()

	time.Sleep(1100 * time.Millisecond)
	was := recencyOf(t, s.sessions, id)
	// **A frame the page never asked for**, which is the shape of a streamed
	// answer as far as this socket is concerned: the desk writes, the page
	// reads, and the page sends nothing at all.
	s.broadcastFileChange("packs/anything.pack.json")
	waitForATouch(t, s.sessions, id, was, "an outbound frame")
}

// TestARefreshedSessionSurvivesTheBound is the property those two directions
// exist for: recency is what eviction reads.
func TestARefreshedSessionSurvivesTheBound(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	watching, err := store.create("local user", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for range maxSessions * 2 {
		if _, err := store.create("local user", nil); err != nil {
			t.Fatalf("create: %v", err)
		}
		store.touch(store.handle(watching))
	}
	if _, ok := store.lookup(watching); !ok {
		t.Fatal("a session refreshed past the bound was evicted anyway")
	}
}

// TestASubprotocolOfferAuthorizesNothingOffTheUpgrade. The offer is the page's
// credential channel **on the handshake**, and the shared gate used to read one
// too. Two paths for one credential is one to keep in step, and a row proved the
// second was already unheld.
func TestASubprotocolOfferAuthorizesNothingOffTheUpgrade(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	for _, route := range []string{"/api/files", "/api/session", "/api/desk-config"} {
		t.Run(route, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, ts.URL+route, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Sec-WebSocket-Protocol", strings.Join(upgradeOffer(id), ", "))
			req.Header.Set("Origin", ts.URL)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s answered %d to a live id offered as a subprotocol, want 401",
					route, resp.StatusCode)
			}
			// The positive control: the same id, on the header this desk reads.
			held, err := http.NewRequest(http.MethodGet, ts.URL+route, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			pageBearer(id)(held)
			held.Header.Set("Origin", ts.URL)
			ok, err := http.DefaultClient.Do(held)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer ok.Body.Close()
			if ok.StatusCode == http.StatusUnauthorized {
				t.Fatalf("%s refuses the id on Authorization too: this test proves nothing", route)
			}
		})
	}
}
