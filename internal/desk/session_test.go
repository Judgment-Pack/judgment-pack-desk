package desk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
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

	// **One cookie, and one only.** Every additional cookie on this desk would
	// be another ambient thing to reason about; there is exactly one, it is the
	// handoff, and it is worth one call to the exchange.
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("the launch set %d cookies, want the handoff alone: %v", len(cookies), cookies)
	}
	cookie := cookies[0]

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

func TestLaunchAcceptsOnlyGET(t *testing.T) {
	s, ts := newTestServer(t, false)
	// **`HEAD` is the one this list used to be missing.** Go's mux treats a
	// `GET` pattern as matching `HEAD`, so `HEAD /launch?secret=…` reached the
	// handler with a valid secret and minted a handoff — a credential handed
	// to a request whose whole contract is that it carries no body, and one no
	// page would ever spend.
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(method, ts.URL+"/launch?secret="+testToken, nil)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /launch answered %d, want 405", method, resp.StatusCode)
		}
		for _, cookie := range resp.Cookies() {
			if cookie.Value != "" {
				t.Fatalf("%s /launch set %v", method, cookie)
			}
		}
	}
	if n := s.launches.count(); n != 0 {
		t.Fatalf("%d handoff(s) were minted by a method that is not GET", n)
	}
	// The control: GET still works.
	if h := launchHandoff(t, ts); h.Value == "" {
		t.Fatal("GET /launch stopped minting a handoff")
	}
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

/* The exchange ---------------------------------------------------------------- */

// TestTheHandoffIsSingleUse — the property that **bounds** the residual. A
// second `POST` with the same cookie is a `401`, so one handoff buys one
// session and no more, however many callers present it. It makes nothing
// visible: the page's own second `POST` reads `handoff-spent`, which is what
// its own earlier spend reads as, so a tab that already holds an id keeps it
// and goes on working alongside whoever took the other session.
func TestTheHandoffIsSingleUse(t *testing.T) {
	s, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)

	id := exchange(t, ts, handoff)
	if id == "" {
		t.Fatal("the first exchange answered no id")
	}
	// **Spent means gone from the store**, which is what makes the second
	// attempt below a refusal rather than a coincidence. A row that made the
	// exchange ignore `consume`'s answer left this assertion as the only thing
	// that could notice.
	if n := s.launches.count(); n != 0 {
		t.Fatalf("%d handoff(s) survive a successful exchange, want 0", n)
	}
	status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if status != http.StatusUnauthorized {
		t.Fatalf("the second exchange answered %d, want 401: %v", status, body)
	}
	// **`handoff-spent`, not `no-handoff`.** A cookie was presented and this
	// desk no longer holds it. The two are separated so that a spend can be
	// told from an *expiry*, which is the one refusal a person can act on;
	// `handoff-spent` itself tells the page nothing, because this desk cannot
	// tell a tab's own earlier spend from anybody else's.
	if body["code"] != CodeHandoffSpent {
		t.Errorf("code %v, want %s", body["code"], CodeHandoffSpent)
	}
	if n := s.sessions.count(); n != 1 {
		t.Fatalf("%d sessions after one spent handoff, want 1", n)
	}
}

// TestTheExchangeClassifiesWhatWasPresented.
//
// Three answers, and they exist because **one** of them says something a person
// can act on. `no-handoff` is nothing this desk recognises, and overwhelmingly a
// reload. `handoff-spent` is a handoff it finished with, which tells the page
// nothing — it cannot distinguish a page's own earlier spend from anybody
// else's, and neither can the page. `handoff-expired` is the one a person can
// act on: nobody used the link, and the secret still works.
func TestTheExchangeClassifiesWhatWasPresented(t *testing.T) {
	_, ts := newTestServer(t, false)

	// A reload: no cookie at all, because the exchange that spent it cleared it.
	noCookie, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if noCookie != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
		t.Fatalf("an absent handoff answered %d %v, want 401 %s", noCookie, body["code"], CodeNoHandoff)
	}

	// Spent: a cookie this desk finished with.
	handoff := launchHandoff(t, ts)
	exchange(t, ts, handoff)
	spent, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if spent != http.StatusUnauthorized || body["code"] != CodeHandoffSpent {
		t.Fatalf("a spent handoff answered %d %v, want 401 %s", spent, body["code"], CodeHandoffSpent)
	}

	// An **expiry** is the third, and `TestTheHandoffExpires` drives it with the
	// clock injected.

	// And the control, so that none of the above passes on a desk that refuses
	// every exchange: a fresh handoff still buys a session.
	fresh, ok := exchangeAttempt(t, ts, withHandoff(ts, launchHandoff(t, ts)))
	if fresh != http.StatusOK {
		t.Fatalf("a fresh handoff answered %d, want 200: %v", fresh, ok)
	}
}

// TestOnlyASuccessfulExchangeClearsTheHandoff.
//
// **A refusal that cleared the cookie would destroy the classification**, which
// is the property this pair of assertions holds. The clearing header and the
// refusal travel in one response, so a tab that reloaded after the browser
// stored the first and before the page had handled the second would present
// **nothing** and read `no-handoff` — a spent or expired handoff turned into an
// unknown one by the desk's own answer, and `handoff-expired` is the one code a
// person can act on. Only success clears now; a stale cookie is bounded by its
// own sixty-second `Max-Age` and reads as spent until then, however many times
// the page loads.
func TestOnlyASuccessfulExchangeClearsTheHandoff(t *testing.T) {
	_, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)

	// The success clears it.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
	withHandoff(ts, handoff)(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	defer resp.Body.Close()
	cleared := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == handoff.Name && cookie.MaxAge < 0 && cookie.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("a successful exchange did not clear the handoff: %v", resp.Cookies())
	}

	// **And the refusal does not**, so a reload inside the delivery window
	// presents the same cookie and reads the same verdict.
	for attempt := range 3 {
		again, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
		withHandoff(ts, handoff)(again)
		refused, err := http.DefaultClient.Do(again)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		defer refused.Body.Close()
		if refused.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d answered %d, want 401", attempt, refused.StatusCode)
		}
		if got := refused.Header.Get(RefusalHeader); got != CodeHandoffSpent {
			t.Fatalf("attempt %d is marked %q, want %q", attempt, got, CodeHandoffSpent)
		}
		for _, cookie := range refused.Cookies() {
			if cookie.Name == handoff.Name {
				t.Fatalf("a refusal cleared the handoff: %v", cookie)
			}
		}
	}
}

// TestABogusCookieFromASiblingPortIsIgnored.
//
// A page on any other loopback port can set `jpack-desk-launch-<port>` at a
// **longer path**, and the browser sends that one first. A rule that read one
// cookie and refused what it did not recognise answered `handoff-spent` on
// every load, for ever, to a tab whose session was perfectly good — and a
// restart on the same port did not repair it, because the planted cookie is
// still there.
func TestABogusCookieFromASiblingPortIsIgnored(t *testing.T) {
	s, ts := newTestServer(t, false)
	bogus := &http.Cookie{Name: s.launchCookie, Value: "not-a-handoff-this-desk-ever-minted"}

	// Alone: ignored entirely, which is `no-handoff` — so a page keeps its id.
	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(bogus)
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
		t.Fatalf("a planted cookie answered %d %v, want 401 %s", status, body["code"], CodeNoHandoff)
	}

	// An empty value is ignored the same way — that is also the shape this
	// desk's own cleared cookie takes when it arrives late.
	status, body = exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: s.launchCookie, Value: ""})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
		t.Fatalf("an empty cookie answered %d %v, want 401 %s", status, body["code"], CodeNoHandoff)
	}

	// **Beside a live one, the live one wins**, whichever order they arrive in:
	// the bogus one is planted at a longer path, so a browser sends it first.
	handoff := launchHandoff(t, ts)
	status, body = exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(bogus)
		r.AddCookie(&http.Cookie{Name: handoff.Name, Value: handoff.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusOK {
		t.Fatalf("a live handoff beside a planted cookie answered %d: %v", status, body)
	}
	if id, _ := body["id"].(string); id == "" {
		t.Fatalf("no id: %v", body)
	}
}

// TestAnExpiredHandoffSaysSoRatherThanSayingNothing. An expiry is nobody's
// fault and the launch secret still works, so the answer is to reopen the
// printed URL — the one instruction a refused exchange can honestly give.
func TestAnExpiredHandoffSaysSoRatherThanSayingNothing(t *testing.T) {
	s, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)

	now := time.Now()
	s.launches.mu.Lock()
	s.launches.now = func() time.Time { return now.Add(launchWindow + time.Second) }
	s.launches.mu.Unlock()

	status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if status != http.StatusUnauthorized || body["code"] != CodeHandoffExpired {
		t.Fatalf("an expired handoff answered %d %v, want 401 %s",
			status, body["code"], CodeHandoffExpired)
	}
	// And it stays expired rather than becoming unknown on the next load.
	status, body = exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if status != http.StatusUnauthorized || body["code"] != CodeHandoffExpired {
		t.Fatalf("a second load answered %d %v, want %s", status, body["code"], CodeHandoffExpired)
	}
}

// TestTheTombstoneRingIsBoundedAndSaysWhatThatCosts.
//
// What the ring buys is one distinction — spent from expired — because those
// two send a person to do different things. Remembering for ever is a map that
// grows for the life of the process; past the bound the oldest record goes and
// its value reads as unknown.
//
// **What that costs is nothing a page acts on**, which is what this asserts: a
// spend that has been cycled out reads as `no-handoff` instead, and the page
// follows the same instruction for both.
func TestTheTombstoneRingIsBoundedAndSaysWhatThatCosts(t *testing.T) {
	s, ts := newTestServer(t, false)

	spent := launchHandoff(t, ts)
	exchange(t, ts, spent)

	// Immediately: it reads as spent.
	status, body := exchangeAttempt(t, ts, withHandoff(ts, spent))
	if status != http.StatusUnauthorized || body["code"] != CodeHandoffSpent {
		t.Fatalf("read %d %v, want %s", status, body["code"], CodeHandoffSpent)
	}

	// Now the secret holder cycles the ring. **Launches and not exchanges**:
	// the unspent-handoff map is itself bounded, so each launch past that bound
	// tombstones the oldest — which fills the ring without minting a session
	// per turn and running into the session bound instead. Either route gets
	// there; this one isolates the ring.
	for range maxTombstones + maxLaunches + 2 {
		launchHandoff(t, ts)
	}
	if n := s.launches.remembered(); n > maxTombstones {
		t.Fatalf("the ring holds %d, past its bound of %d", n, maxTombstones)
	}
	if n := s.launches.remembered(); n != maxTombstones {
		t.Fatalf("the ring holds %d, want it full at %d — the cycle did not reach the bound",
			n, maxTombstones)
	}

	// **And the spent record is gone**, so the same value now reads as nothing.
	// The page follows the same instruction either way, which is why this is a
	// cost worth naming and not a defect worth guarding.
	status, body = exchangeAttempt(t, ts, withHandoff(ts, spent))
	if status != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
		t.Fatalf("after the ring cycled, read %d %v, want %s", status, body["code"], CodeNoHandoff)
	}

	// **The same, for an expired record**, which is the arm that matters to a
	// tab holding an id: `handoff-expired` before the eviction and `no-handoff`
	// after it, and the page keeps its id on both. The reopen line is for a tab
	// with no session; `web/src/mcp/session.test.tsx` holds that half.
	lapsed := launchHandoff(t, ts)
	now := time.Now()
	s.launches.mu.Lock()
	s.launches.now = func() time.Time { return now.Add(launchWindow + time.Second) }
	s.launches.mu.Unlock()

	status, body = exchangeAttempt(t, ts, withHandoff(ts, lapsed))
	if status != http.StatusUnauthorized || body["code"] != CodeHandoffExpired {
		t.Fatalf("a lapsed handoff read %d %v, want %s", status, body["code"], CodeHandoffExpired)
	}

	for range maxTombstones + maxLaunches + 2 {
		launchHandoff(t, ts)
	}
	status, body = exchangeAttempt(t, ts, withHandoff(ts, lapsed))
	if status != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
		t.Fatalf("after the ring cycled, a lapsed handoff read %d %v, want %s",
			status, body["code"], CodeNoHandoff)
	}
}

// TestTheExchangeRefusesWithoutAHandoff: no cookie at all is the state a page
// is in when nobody opened the printed URL, and it is the same refusal a spent
// one gets — one sentence, and it names the way out.
func TestTheExchangeRefusesWithoutAHandoff(t *testing.T) {
	s, ts := newTestServer(t, false)
	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %v", status, body)
	}
	if body["code"] != CodeNoHandoff {
		t.Errorf("code %v, want %s", body["code"], CodeNoHandoff)
	}
	if message, _ := body["error"].(string); !strings.Contains(message, "printed at startup") {
		t.Errorf("the refusal does not name the way out: %q", message)
	}
	if n := s.sessions.count(); n != 0 {
		t.Fatalf("a refused exchange minted %d session(s)", n)
	}
}

// TestTheExchangeClearsTheHandoff: spent, and said so on the wire, so the
// browser drops a value that is already worthless.
func TestTheExchangeClearsTheHandoff(t *testing.T) {
	_, ts := newTestServer(t, false)
	handoff := launchHandoff(t, ts)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
	withHandoff(ts, handoff)(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	defer resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("the exchange set %v, want the handoff cleared and nothing else", cookies)
	}
	cleared := cookies[0]
	if cleared.Name != handoff.Name {
		t.Errorf("cleared %q, want %q", cleared.Name, handoff.Name)
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("Max-Age = %d, want a negative one that clears it", cleared.MaxAge)
	}
	if cleared.Value != "" {
		t.Errorf("the cleared cookie carries %q", cleared.Value)
	}
	// **And it must not be `Secure` over plain http**, or it clears nothing: a
	// browser refuses a `Secure` cookie from a non-secure origin, so the
	// handoff would survive the request that spent it and the page would try to
	// spend a handoff that is gone on every load. This desk is served over http
	// on loopback.
	if cleared.Secure {
		t.Error("Secure on a plain-http expiry, which a browser refuses")
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

	status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if status != http.StatusUnauthorized {
		t.Fatalf("an expired handoff answered %d, want 401: %v", status, body)
	}
	if body["code"] != CodeHandoffExpired {
		t.Errorf("code %v, want %s", body["code"], CodeHandoffExpired)
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
// `Sec-Fetch-Site: same-origin` takes the session first, and this desk **cannot
// tell that session from the person's own**. Forbidden-header rules bind
// browsers, not scripts. What the shape buys is the bound and nothing more:
// sixty seconds, and one use, so the stolen handoff buys exactly one session
// and the window closes. The desk does not detect it and does not announce it;
// the remedy is a restart.
func TestTheStatedResidual(t *testing.T) {
	_, ts := newTestServer(t, false)

	// **An authenticated tab**, which is the case the first version of this
	// test missed. The tab already holds a working session; the person then
	// reopens the printed URL, and a script takes the handoff that launch set
	// before the page can spend it.
	held := beginSession(t, ts)
	acceptsSession(t, ts, held, ts.URL)
	handoff := launchHandoff(t, ts)

	// The thief, inside the window, forging the one header a browser would not
	// let a page write.
	stolen, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if stolen != http.StatusOK {
		t.Fatalf("the residual is not what this test says it is: status %d", stolen)
	}
	if id, _ := body["id"].(string); id == "" {
		t.Fatal("the thief got no id")
	}

	// And the page's own exchange then fails, `handoff-spent`, because the
	// handoff is single use. **The page reads that exactly as `no-handoff`**:
	// the desk cannot tell a thief's spend from this tab's own earlier one, so
	// neither may the page. `web/src/mcp/session.test.tsx` holds that half —
	// an authenticated tab keeps its id, a fresh tab has no session.
	after, refusal := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if after != http.StatusUnauthorized {
		t.Fatalf("the page's own exchange answered %d after a theft, want 401", after)
	}
	if refusal["code"] != CodeHandoffSpent {
		t.Fatalf("code %v after a theft, want %s", refusal["code"], CodeHandoffSpent)
	}
	// The old id is still live on the wire — the chassis forgets nothing — and
	// the tab goes on using it, alongside the thief, until the desk restarts.
	acceptsSession(t, ts, held, ts.URL)
}

/* Nothing ambient authorizes anything ------------------------------------------ */

// TestNoRouteEverSetsASessionCookie sweeps every route this chassis has and
// asserts that **no answer carries a live cookie** except the launch's handoff.
//
// **Each route gets its own session, and each is required to succeed.**
// Asserting the success status is what makes "this handler set no cookie" a
// statement about the handler rather than about the guard in front of it: a
// `401` sets no cookies either, and would pass this for the wrong reason.
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
		plain(http.MethodGet, "/", http.StatusNotFound),
		plain(http.MethodGet, "/packs/anything", http.StatusNotFound),
		{"POST /api/session", http.StatusOK, func(t *testing.T, _ string) *http.Response {
			t.Helper()
			// The exchange has its own shape: a handoff and the same-origin
			// claim, which is the only route on this desk that takes either.
			// It clears the handoff it spends, which is an expiry and not a
			// live cookie — the assertion below reads `MaxAge`.
			handoff := launchHandoff(t, ts)
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
			withHandoff(ts, handoff)(req)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		{"GET /ws", http.StatusSwitchingProtocols, func(t *testing.T, id string) *http.Response {
			t.Helper()
			return upgradeRequest(t, ts, "", ts.URL, withOffer(id))
		}},
	} {
		t.Run(route.name, func(t *testing.T) {
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
// **This is the round-2 finding on PR #40, held.** A cookie replayed by a
// script with a forged `Sec-Fetch-Site` used to read this desk; there is
// nothing to replay now, because no cookie is consulted anywhere but the
// exchange.
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
		{http.MethodPost, "/api/session"},
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

// TestOnlyTheOfferCarriesASessionOntoTheUpgrade. On `/ws` a session id is read
// off the subprotocol offer and nowhere else: admitting it on `Authorization`
// too would be two ways to open one socket with one credential, and one of them
// is a way the browser cannot even use — a `WebSocket` constructor has no
// header parameter.
func TestOnlyTheOfferCarriesASessionOntoTheUpgrade(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	refused := upgradeRequest(t, ts, "", ts.URL, pageBearer(id))
	if refused.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a session id on the upgrade's Authorization header: status %d, want 401",
			refused.StatusCode)
	}
	// The two positive controls: the launch secret on that header, and the id
	// in the offer where it belongs.
	if resp := upgradeRequest(t, ts, "", ts.URL, bearer); resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("the launch secret does not open the relay")
	}
	acceptsSession(t, ts, id, ts.URL)
}

// TestASubprotocolOfferAuthorizesNothingOffTheUpgrade. The offer is the page's
// credential channel **on the handshake**, and only there.
func TestASubprotocolOfferAuthorizesNothingOffTheUpgrade(t *testing.T) {
	_, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	for _, route := range []string{"/api/files", "/api/session", "/api/desk-config"} {
		t.Run(route, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, ts.URL+route, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set(wsProtocolHeader, strings.Join(upgradeOffer(id), ", "))
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

// TestTheOriginGuardStandsOnEveryBearerRoute. The guard is defence in depth on
// a gated route — a cross-site page holds no bearer to send — and it is still
// not optional, because defence in depth that is removed is not defence.
func TestTheOriginGuardStandsOnEveryBearerRoute(t *testing.T) {
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
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req, err := http.NewRequest(route.method, ts.URL+route.path, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			pageBearer(id)(req)
			req.Header.Set("Origin", "http://evil.example")
			req.Header.Set("Content-Type", "application/json")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status %d, want 403", resp.StatusCode)
			}
		})
	}
	// And the upgrade, which is the route the guard was written for.
	t.Run("GET /ws", func(t *testing.T) {
		resp := upgradeRequest(t, ts, "", "http://evil.example", withOffer(id))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d, want 403", resp.StatusCode)
		}
	})
}

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

// thePage is what the stand-in single-page shell carries, so a test can tell
// "the fallback served the page" from "there was nothing to serve".
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

// TestALenientDecodeSeesPastOneBadEscape, beside the end-to-end rows above:
// `url.QueryUnescape` returns nothing at all when one escape anywhere is
// invalid, and that silence was the hole.
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
	if querySmellsOfASecret("%25%37%33ecret=x") {
		t.Error("a second decode pass was made")
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

/* The stores -------------------------------------------------------------------- */

func TestTheStoreHoldsNoSessionID(t *testing.T) {
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

// TestTheSessionStoreRefusesPastItsBound. The bound is a **refusal**: nothing
// is dropped to make room, so a session somebody is using is never ended from
// the outside — which is what would need a second actor in the page to notice.
func TestTheSessionStoreRefusesPastItsBound(t *testing.T) {
	store, err := newSessionStore()
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	ids := make([]string, 0, maxSessions)
	for range maxSessions {
		id, err := store.create("local user", nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, id)
	}
	if n := store.count(); n != maxSessions {
		t.Fatalf("the store holds %d, want it full at %d", n, maxSessions)
	}
	if _, err := store.create("local user", nil); err == nil {
		t.Fatal("the store minted a session past its bound")
	} else if !strings.Contains(err.Error(), "maximum of sessions") {
		t.Fatalf("the refusal is %q, want the one a person can act on", err)
	}
	if n := store.count(); n != maxSessions {
		t.Fatalf("a refused create changed the count to %d", n)
	}
	// **Every session that was there is still there**, which is the difference
	// between refusing and evicting.
	for i, id := range ids {
		if _, ok := store.lookup(id); !ok {
			t.Fatalf("session %d was dropped to make room for one that was refused", i)
		}
	}
}

// TestTheBoundIsRefusedThroughTheExchange is the same property at the wire: the
// 65th exchange answers `503` and the sentence a person can act on, and every
// session already minted goes on working.
func TestTheBoundIsRefusedThroughTheExchange(t *testing.T) {
	s, ts := newTestServer(t, false)
	first := ""
	for i := range maxSessions {
		id := beginSession(t, ts)
		if i == 0 {
			first = id
		}
	}
	if n := s.sessions.count(); n != maxSessions {
		t.Fatalf("%d live sessions after %d exchanges", n, maxSessions)
	}

	handoff := launchHandoff(t, ts)
	status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("the 65th exchange answered %d, want 503: %v", status, body)
	}
	if body["code"] != CodeSessionsFull {
		t.Errorf("code %v, want %s", body["code"], CodeSessionsFull)
	}
	if message, _ := body["error"].(string); message != "this desk holds its maximum of sessions; restart it" {
		t.Errorf("the refusal reads %q", message)
	}
	if n := s.sessions.count(); n != maxSessions {
		t.Fatalf("%d live sessions after a refusal", n)
	}
	// And the first session is still the first session: nothing was evicted to
	// make room for the one that was refused.
	acceptsSession(t, ts, first, ts.URL)
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

/* GET /api/session ---------------------------------------------------------------- */

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
	// **Two members, and no more.** This route reported a count of the sessions
	// this process had minted, so that a page could notice one it did not ask
	// for. **That whole line of work is withdrawn** — four rounds, four defects
	// on one seam — and a route that still carried the number would be inviting
	// the next reader to try again.
	if len(body) != 2 {
		t.Errorf("the answer carries %d members, want subject and issuer only: %v",
			len(body), body)
	}
	if _, present := body["sessions"]; present {
		t.Errorf("the answer still carries a sessions member: %v", body)
	}
}

// TestSessionEndpointSaysAScriptIsNobody. A script authorizing with the launch
// secret holds no session record, and the answer says so rather than inventing
// one: the secret is a way in, not somebody this desk authenticated.
func TestSessionEndpointSaysAScriptIsNobody(t *testing.T) {
	_, ts := newTestServer(t, false)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/session", nil)
	bearer(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["subject"] != "" || body["issuer"] != nil {
		t.Errorf("a script's record is %v, want an empty subject and a null issuer", body)
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

// TestASessionEndsOnlyWithTheProcess. There is no route that ends a session, so
// a `DELETE` is not a sign-out — it falls to the reader, which answers the
// bearer's record — and the session is still live afterwards. Sign-out arrives
// with the identity provider, whose sign-out it will be.
func TestASessionEndsOnlyWithTheProcess(t *testing.T) {
	s, ts := newTestServer(t, false)
	id := beginSession(t, ts)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/session", nil)
	pageBearer(id)(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if _, forgotten := body["forgotten"]; forgotten {
		t.Fatalf("DELETE /api/session is a sign-out: %v", body)
	}
	if n := s.sessions.count(); n != 1 {
		t.Fatalf("%d sessions after a DELETE, want the one that was minted", n)
	}
	acceptsSession(t, ts, id, ts.URL)
}

/* The mark this chassis puts on its own refusals ------------------------------ */

// TestEveryRefusalThisChassisAuthoredIsMarked.
//
// **Why a header rather than the body.** One route forwards somebody else's
// answer — the model relay — so a `401` there is either this desk refusing the
// page's session or the configured endpoint refusing the stored key. The page
// has to tell them apart, and reading the body to do it was wrong twice: a
// cloned body has tee semantics, so an oversized chunked answer deadlocks the
// reader classifying it, and an endpoint can write any body it likes, so the
// discriminator was forgeable. This header is set by `writeJSONCoded` and
// `refuseText`, and stripped from every upstream answer.
func TestEveryRefusalThisChassisAuthoredIsMarked(t *testing.T) {
	s, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", "{}")
	id := beginSession(t, ts)

	type want struct {
		name   string
		status int
		code   string
		do     func(t *testing.T) *http.Response
	}
	plain := func(name, path, code string, status int, decorate ...func(*http.Request)) want {
		return want{name, status, code, func(t *testing.T) *http.Response {
			t.Helper()
			req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			for _, apply := range decorate {
				apply(req)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}}
	}
	for _, each := range []want{
		// The shared guard, both halves.
		plain("the guard, no session", "/api/files", CodeUnauthorized, http.StatusUnauthorized),
		plain("the guard, a foreign origin", "/api/files", CodeForbidden, http.StatusForbidden,
			pageBearer(id), func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }),
		// The launch path, in plain text.
		plain("a wrong launch secret", "/launch?secret=wrong", CodeForbidden, http.StatusForbidden),
		plain("under the launch path", "/launch/anything", CodeNotFound, http.StatusNotFound),
		// The exchange's own two.
		{"no handoff", http.StatusUnauthorized, CodeNoHandoff, func(t *testing.T) *http.Response {
			t.Helper()
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
			req.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
			req.Header.Set("Origin", ts.URL)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		{"a spent handoff", http.StatusUnauthorized, CodeHandoffSpent, func(t *testing.T) *http.Response {
			t.Helper()
			handoff := launchHandoff(t, ts)
			exchange(t, ts, handoff)
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
			withHandoff(ts, handoff)(req)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		// **The writer that went round the back of the mark.**
		// `writeJSONError` reached `writeJSON` directly, so a `GET /api/file`
		// with no `path` answered a refusal the page could not tell from an
		// endpoint's. The mark is set in `writeJSON` now, which is the one
		// function every JSON answer goes through.
		plain("an authenticated read with no path", "/api/file", CodeBadRequest,
			http.StatusBadRequest, pageBearer(id)),
		// The static handler's own 404s, which are chassis-authored too.
		plain("a missing asset", "/nothing.js", CodeNotFound, http.StatusNotFound),
		// **A structured refusal body**, whose code lived in a struct rather
		// than a map: the mark read a type switch over the shapes somebody
		// remembered, and called this one a generic `refused` while its body
		// named `stale`.
		{"a stale write", http.StatusConflict, CodeStale, func(t *testing.T) *http.Response {
			t.Helper()
			payload, _ := json.Marshal(WriteRequest{
				Path: "jpack.json", Content: "{}", BaseSHA256: strings.Repeat("a", 64),
			})
			req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file", bytes.NewReader(payload))
			pageBearer(id)(req)
			req.Header.Set("Content-Type", "application/json")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		// The desk-level decoder's own refusal, whose body carries a problem
		// list beside its code.
		{"an invalid configuration", http.StatusUnprocessableEntity, CodeDeskConfigRefused,
			func(t *testing.T) *http.Response {
				t.Helper()
				req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/desk-config",
					strings.NewReader(`{"ifMatch":"","assistant":{"endpoint":`+
						`{"kind":"nothing-defines-this","url":"https://example.invalid",`+
						`"model":"m","tools":["validate"]}}}`))
				pageBearer(id)(req)
				req.Header.Set("Content-Type", "application/json")
				resp, err := ts.Client().Do(req)
				if err != nil {
					t.Fatalf("do: %v", err)
				}
				return resp
			}},
		// The exchange's third code.
		{"an expired handoff", http.StatusUnauthorized, CodeHandoffExpired, func(t *testing.T) *http.Response {
			t.Helper()
			handoff := launchHandoff(t, ts)
			now := time.Now()
			s.launches.mu.Lock()
			was := s.launches.now
			s.launches.now = func() time.Time { return now.Add(launchWindow + time.Second) }
			s.launches.mu.Unlock()
			t.Cleanup(func() {
				s.launches.mu.Lock()
				s.launches.now = was
				s.launches.mu.Unlock()
			})
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
			withHandoff(ts, handoff)(req)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		// HEAD on the launch path.
		{"HEAD /launch", http.StatusMethodNotAllowed, CodeBadRequest, func(t *testing.T) *http.Response {
			t.Helper()
			req, _ := http.NewRequest(http.MethodHead, ts.URL+"/launch?secret="+testToken, nil)
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			return resp
		}},
		// The upgrade, in plain text: no credential, and a malformed offer.
		{"the upgrade, no session", http.StatusUnauthorized, CodeUnauthorized, func(t *testing.T) *http.Response {
			t.Helper()
			return upgradeRequest(t, ts, "", ts.URL)
		}},
		{"the upgrade, two ids", http.StatusBadRequest, CodeBadRequest, func(t *testing.T) *http.Response {
			t.Helper()
			return upgradeRequest(t, ts, "", ts.URL, func(r *http.Request) {
				r.Header.Set(wsProtocolHeader,
					strings.Join([]string{wsProtocol, wsSessionPrefix + id, wsSessionPrefix + id}, ", "))
			})
		}},
	} {
		t.Run(each.name, func(t *testing.T) {
			resp := each.do(t)
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != each.status {
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, each.status, raw)
			}
			if got := resp.Header.Get(RefusalHeader); got != each.code {
				t.Fatalf("%s = %q, want %q", RefusalHeader, got, each.code)
			}
			// **The header says what the body says.** A mark that named a
			// different code from the answer beside it would be a second
			// opinion about one refusal, and the page reads both.
			var named struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(raw, &named); err == nil && named.Code != "" {
				if named.Code != resp.Header.Get(RefusalHeader) {
					t.Fatalf("the body says %q and the header says %q",
						named.Code, resp.Header.Get(RefusalHeader))
				}
			}
		})
	}

	// **The positive control, and it is the point of the header.** An answer
	// this chassis did not refuse carries no mark, so "the header is present"
	// is a statement about refusals and not about every response.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/files", nil)
	pageBearer(id)(req)
	ok, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("the control answered %d, want 200", ok.StatusCode)
	}
	if got := ok.Header.Get(RefusalHeader); got != "" {
		t.Fatalf("a successful answer carries %s = %q", RefusalHeader, got)
	}
	if n := s.sessions.count(); n == 0 {
		t.Fatal("no session was ever minted: the sweep proves nothing")
	}
}

// TestTheSessionsFullRefusalIsMarkedAndSaysWhatToDo is item 4's wire half: the
// page shows this sentence verbatim, because "open the printed URL" cannot help
// a tab that met a desk at its bound.
func TestTheSessionsFullRefusalIsMarkedAndSaysWhatToDo(t *testing.T) {
	s, ts := newTestServer(t, false)
	for range maxSessions {
		beginSession(t, ts)
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
	withHandoff(ts, launchHandoff(t, ts))(req)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", resp.StatusCode)
	}
	if got := resp.Header.Get(RefusalHeader); got != CodeSessionsFull {
		t.Fatalf("%s = %q, want %q", RefusalHeader, got, CodeSessionsFull)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "this desk holds its maximum of sessions; restart it" {
		t.Fatalf("the refusal reads %q", body["error"])
	}
	if n := s.sessions.count(); n != maxSessions {
		t.Fatalf("%d sessions after the refusal, want the bound of %d", n, maxSessions)
	}
}

// TestTheBoundIsAskedAfterTheCookieIsClassified.
//
// **Order, and it is the whole of one defect.** Asking the bound first meant an
// ordinary reload at a full desk — no handoff presented, and `no-handoff` is
// what it would have been told — was answered `503` instead. A page that acts
// on a capacity refusal deletes an id that was perfectly good, so only a
// **live** handoff, which is the only thing that can mint, may be refused for
// want of room.
func TestTheBoundIsAskedAfterTheCookieIsClassified(t *testing.T) {
	fill := func(t *testing.T) (*Server, *httptest.Server) {
		t.Helper()
		s, ts := newTestServer(t, false)
		for range maxSessions {
			exchangeAttempt(t, ts, bearer)
		}
		return s, ts
	}

	t.Run("a live handoff is refused, and not eaten", func(t *testing.T) {
		s, ts := fill(t)
		handoff := launchHandoff(t, ts)
		before := s.launches.count()

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
		withHandoff(ts, handoff)(req)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", resp.StatusCode)
		}
		if got := resp.Header.Get(RefusalHeader); got != CodeSessionsFull {
			t.Fatalf("%s = %q, want %q", RefusalHeader, got, CodeSessionsFull)
		}
		// **The cookie is left where it is**, so the link is still usable after
		// a restart: the secret and the store are both per process.
		if cookies := resp.Cookies(); len(cookies) != 0 {
			t.Fatalf("a capacity refusal set %v", cookies)
		}
		if after := s.launches.count(); after != before {
			t.Fatalf("a capacity refusal spent a handoff: %d unspent before, %d after", before, after)
		}
	})

	t.Run("a plain reload is not refused for want of room", func(t *testing.T) {
		_, ts := fill(t)
		status, body := exchangeAttempt(t, ts, func(r *http.Request) {
			r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
			r.Header.Set("Origin", ts.URL)
		})
		if status != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
			t.Fatalf("a reload at a full desk answered %d %v, want 401 %s",
				status, body["code"], CodeNoHandoff)
		}
	})

	t.Run("a spent cookie is not refused for want of room", func(t *testing.T) {
		s, ts := newTestServer(t, false)
		handoff := launchHandoff(t, ts)
		exchange(t, ts, handoff)
		for s.sessions.count() < maxSessions {
			exchangeAttempt(t, ts, bearer)
		}
		status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
		if status != http.StatusUnauthorized || body["code"] != CodeHandoffSpent {
			t.Fatalf("a spent cookie at a full desk answered %d %v, want 401 %s",
				status, body["code"], CodeHandoffSpent)
		}
	})

	t.Run("an expired cookie is not refused for want of room", func(t *testing.T) {
		s, ts := newTestServer(t, false)
		handoff := launchHandoff(t, ts)
		now := time.Now()
		s.launches.mu.Lock()
		s.launches.now = func() time.Time { return now.Add(launchWindow + time.Second) }
		s.launches.mu.Unlock()
		for s.sessions.count() < maxSessions {
			exchangeAttempt(t, ts, bearer)
		}
		status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
		if status != http.StatusUnauthorized || body["code"] != CodeHandoffExpired {
			t.Fatalf("an expired cookie at a full desk answered %d %v, want 401 %s",
				status, body["code"], CodeHandoffExpired)
		}
	})

	t.Run("an unknown cookie is not refused for want of room", func(t *testing.T) {
		s, ts := fill(t)
		status, body := exchangeAttempt(t, ts, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: s.launchCookie, Value: "never-minted-here"})
			r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
			r.Header.Set("Origin", ts.URL)
		})
		if status != http.StatusUnauthorized || body["code"] != CodeNoHandoff {
			t.Fatalf("an unknown cookie at a full desk answered %d %v, want 401 %s",
				status, body["code"], CodeNoHandoff)
		}
	})

	t.Run("a script's bearer mint is refused", func(t *testing.T) {
		_, ts := fill(t)
		status, body := exchangeAttempt(t, ts, bearer)
		if status != http.StatusServiceUnavailable || body["code"] != CodeSessionsFull {
			t.Fatalf("a script at a full desk answered %d %v, want 503 %s",
				status, body["code"], CodeSessionsFull)
		}
	})
}

// oneExchange is one `POST /api/session` made from a goroutine, so a barrier
// test can hold two of them at once. It reports rather than fails, because
// `t.Fatalf` from a goroutine is not `t.Fatalf`.
type oneExchange struct {
	status  int
	code    string
	cookies []*http.Cookie
	err     error
}

func exchangeFrom(ts *httptest.Server, decorate func(*http.Request)) oneExchange {
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/session", nil)
	if err != nil {
		return oneExchange{err: err}
	}
	decorate(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oneExchange{err: err}
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	code, _ := body["code"].(string)
	return oneExchange{status: resp.StatusCode, code: code, cookies: resp.Cookies()}
}

// holdEveryExchangeAtTheCommitment stops each exchange at the instant it has
// classified its handoff and read the bound and has not yet committed, and
// answers a channel that reports each arrival and a function that releases
// them all.
func holdEveryExchangeAtTheCommitment(t *testing.T, expect int) (<-chan struct{}, func()) {
	t.Helper()
	arrived := make(chan struct{}, expect)
	release := make(chan struct{})
	testHookBeforeCommit = func() {
		arrived <- struct{}{}
		<-release
	}
	t.Cleanup(func() { testHookBeforeCommit = nil })
	return arrived, func() { close(release) }
}

// TestTheLastSlotIsMintedOnceAndTheOtherLinkSurvives.
//
// **The interleaving the commitment point exists for.** At sixty-three
// sessions, two requests each carrying a live handoff both read a bound with
// room in it. Spending before minting, both would eat their handoffs and one
// would then be refused for want of room — a `503` that also destroyed a launch
// link the person could otherwise still use after a restart.
//
// The bound, the spend and the write are one critical section, so exactly one
// mints and the other refuses **without spending**: its cookie is untouched,
// its handoff is still live in the store, and nothing was set on the way out.
func TestTheLastSlotIsMintedOnceAndTheOtherLinkSurvives(t *testing.T) {
	s, ts := newTestServer(t, false)
	for s.sessions.count() < maxSessions-1 {
		exchangeAttempt(t, ts, bearer)
	}
	if n := s.sessions.count(); n != maxSessions-1 {
		t.Fatalf("%d sessions, want %d before the race", n, maxSessions-1)
	}
	first := launchHandoff(t, ts)
	second := launchHandoff(t, ts)

	arrived, release := holdEveryExchangeAtTheCommitment(t, 2)
	answers := make(chan oneExchange, 2)
	for _, handoff := range []*http.Cookie{first, second} {
		go func(handoff *http.Cookie) {
			answers <- exchangeFrom(ts, withHandoff(ts, handoff))
		}(handoff)
	}
	// Both are past the classification and the early bound, and neither has
	// committed. Releasing them here is what makes the interleaving the one
	// under test rather than whichever one the scheduler picked.
	<-arrived
	<-arrived
	release()

	minted, refused := 0, 0
	for range 2 {
		got := <-answers
		if got.err != nil {
			t.Fatalf("exchange: %v", got.err)
		}
		switch got.status {
		case http.StatusOK:
			minted++
		case http.StatusServiceUnavailable:
			refused++
			if got.code != CodeSessionsFull {
				t.Errorf("the refusal read %q, want %s", got.code, CodeSessionsFull)
			}
			if len(got.cookies) != 0 {
				t.Errorf("a capacity refusal set %v", got.cookies)
			}
		default:
			t.Fatalf("an exchange answered %d %q", got.status, got.code)
		}
	}
	if minted != 1 || refused != 1 {
		t.Fatalf("%d minted and %d refused, want exactly one of each", minted, refused)
	}
	if n := s.sessions.count(); n != maxSessions {
		t.Fatalf("%d sessions, want %d", n, maxSessions)
	}
	// **One handoff spent, one still live.** The refused request kept its link,
	// which is the whole point of refusing before committing.
	if n := s.launches.count(); n != 1 {
		t.Fatalf("%d unspent handoff(s), want the refused request's one", n)
	}
	live := 0
	for _, handoff := range []*http.Cookie{first, second} {
		if s.launches.classify(handoff.Value) == handoffAccepted {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("%d of the two handoffs are still live, want 1", live)
	}
}

// TestASlotTakenAfterTheClassificationRefusesWithoutSpending is the other order
// Codex named: this request classifies a live handoff and reads a bound with
// room, and another exchange fills the last slot before it commits.
//
// It must answer `sessions-full` **with its handoff unspent**, and it must not
// clear the tab's cookie: a page that acted on a capacity refusal by deleting
// its id would delete one that is perfectly good.
func TestASlotTakenAfterTheClassificationRefusesWithoutSpending(t *testing.T) {
	s, ts := newTestServer(t, false)
	for s.sessions.count() < maxSessions-1 {
		exchangeAttempt(t, ts, bearer)
	}
	handoff := launchHandoff(t, ts)

	// The hook fires once, on the request under test, and fills the last slot
	// from underneath it — the store's own door, so nothing here depends on a
	// second request winning a race.
	var once sync.Once
	testHookBeforeCommit = func() {
		once.Do(func() {
			if _, err := s.sessions.create("local user", nil); err != nil {
				t.Errorf("filling the last slot: %v", err)
			}
		})
	}
	t.Cleanup(func() { testHookBeforeCommit = nil })

	status, body := exchangeAttempt(t, ts, withHandoff(ts, handoff))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %v", status, body)
	}
	if body["code"] != CodeSessionsFull {
		t.Fatalf("code %v, want %s", body["code"], CodeSessionsFull)
	}
	if n := s.launches.count(); n != 1 {
		t.Fatalf("%d unspent handoff(s), want the refused request's one", n)
	}
	if s.launches.classify(handoff.Value) != handoffAccepted {
		t.Fatal("the refused request spent its handoff")
	}
}

// TestTheHandlersThatWriteTheirOwnRefusalsAreMarkedToo.
//
// Two refusals on this desk are written by libraries rather than by
// `writeJSON`: `websocket.Accept` answers its own `400` to a handshake it will
// not read, and `http.FileServer` answers its own `416` to a byte range nothing
// satisfies. Both are chassis-authored — this desk chose to serve them — and
// both were unmarked until the ResponseWriter they are handed marks them.
func TestTheHandlersThatWriteTheirOwnRefusalsAreMarkedToo(t *testing.T) {
	s, ts := deskWithAPage(t)
	id := beginSession(t, ts)
	_ = s

	t.Run("an authenticated malformed handshake", func(t *testing.T) {
		// Authorized by the offer, and then refused by the library: no
		// `Sec-WebSocket-Key`, which this desk's own gate does not read.
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/ws", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Origin", ts.URL)
		withOffer(id)(req)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", resp.StatusCode)
		}
		if got := resp.Header.Get(RefusalHeader); got == "" {
			t.Fatal("the library's own refusal is unmarked")
		}
	})

	t.Run("an invalid asset range", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/index.html", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		// Far past the file, which `http.ServeContent` answers with a 416.
		req.Header.Set("Range", "bytes=99999-100000")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("status %d, want 416", resp.StatusCode)
		}
		if got := resp.Header.Get(RefusalHeader); got == "" {
			t.Fatal("the file server's own refusal is unmarked")
		}
	})

	// **The positive control**: a range that *is* satisfiable is not a refusal
	// and carries no mark, so the two rows above are about refusals rather than
	// about every answer this handler writes.
	t.Run("a range that is satisfiable", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/index.html", nil)
		req.Header.Set("Range", "bytes=0-3")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("status %d, want 206", resp.StatusCode)
		}
		if got := resp.Header.Get(RefusalHeader); got != "" {
			t.Fatalf("a 206 carries %s = %q", RefusalHeader, got)
		}
	})
}

// TestEveryLiveHandoffInOneRequestIsSpent.
//
// A request can only carry two live values of this name if something planted
// one at a different path: a second `Set-Cookie` at `Path=/` replaces the
// first. Where it happens, **every live one is spent** and one session is
// minted. The alternative — spend the first and leave the rest live — makes the
// *next* reload mint a second session from a cookie nobody deliberately used,
// which is what this test drives.
func TestEveryLiveHandoffInOneRequestIsSpent(t *testing.T) {
	s, ts := newTestServer(t, false)
	first := launchHandoff(t, ts)
	second := launchHandoff(t, ts)

	status, body := exchangeAttempt(t, ts, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: first.Name, Value: first.Value})
		r.AddCookie(&http.Cookie{Name: second.Name, Value: second.Value})
		r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
		r.Header.Set("Origin", ts.URL)
	})
	if status != http.StatusOK {
		t.Fatalf("two live handoffs answered %d: %v", status, body)
	}
	if n := s.sessions.count(); n != 1 {
		t.Fatalf("%d sessions minted from one exchange, want 1", n)
	}

	// **Neither is live afterwards.** A reload presenting either mints nothing
	// and reads as any other spent handoff, which the page treats as a reload.
	for name, handoff := range map[string]*http.Cookie{"the first": first, "the second": second} {
		after, refusal := exchangeAttempt(t, ts, withHandoff(ts, handoff))
		if after != http.StatusUnauthorized || refusal["code"] != CodeHandoffSpent {
			t.Fatalf("%s handoff answered %d %v on a later load, want 401 %s",
				name, after, refusal["code"], CodeHandoffSpent)
		}
	}
	if n := s.sessions.count(); n != 1 {
		t.Fatalf("%d sessions after the later loads, want still 1", n)
	}
	if n := s.launches.count(); n != 0 {
		t.Fatalf("%d handoff(s) are still live, want none", n)
	}
}
