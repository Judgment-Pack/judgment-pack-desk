package desk

// The bootstrap, the exchange, and the bearer session they produce.
//
// # The rule this file exists to keep
//
// **After the bootstrap, this desk holds no ambient credential.** Nothing the
// browser attaches by itself authorizes anything: every gated request carries a
// session id the page put on it deliberately, and a request that carries none
// is refused however it arrives.
//
// That rule is the answer to a class of defect this desk went through twice.
//
//   - The secret rode on `?token=`, so it was in the address bar, in `Referer`,
//     in proxy logs, in history, and in `Response.url` inside the page. The
//     relay's query rule (`relayQueryProblem`) exists only because of it.
//   - Then the session was a cookie. A cookie is ambient by construction, and —
//     this is the part that is easy to get wrong — **a cookie has no port**. A
//     cookie set for `127.0.0.1` is sent to every port on that host, so every
//     other local service received this desk's session. Naming the cookie for
//     the port stopped two desks colliding; it did not stop the browser sending
//     it. Requiring `Sec-Fetch-Site: same-origin` stopped a *page* on a sibling
//     port; it did nothing about a *script*, because forbidden-header rules bind
//     browsers and nothing else. A captured cookie replayed by `curl` with a
//     forged header read the desk.
//
// So the cookie is not the session any more. It is a **one-shot handoff**, and
// the shape is:
//
//  1. `GET /launch?secret=…` matches the launch secret in constant time and
//     sets `jpack-desk-launch-<port>`: a fresh 192-bit value, single use, good
//     for sixty seconds, `HttpOnly; SameSite=Strict; Path=/; Max-Age=60`. It
//     mints no session. Then `303` to `/#`.
//  2. The page loads and calls `POST /api/session` **once**. That request
//     consumes the launch cookie — it is marked used and cleared with
//     `Max-Age=0` — and answers a 192-bit **session id** in the body.
//  3. The page keeps that id in `sessionStorage`, under a key that includes the
//     origin's port, and puts it on every request itself: `Authorization:
//     Bearer <id>` on `fetch`, and the WebSocket subprotocol offer on the
//     upgrade. It is never on a URL and never in a cookie.
//
// The window in which anything ambient exists is therefore one request wide and
// sixty seconds long, and what is ambient in it is not the session.
//
// # The residual, stated
//
// A script that captures the launch cookie **inside that window** and forges
// `Sec-Fetch-Site: same-origin` can take the session before the page does.
// Forbidden-header rules stop browsers, not scripts, and nothing this desk can
// write changes that. What the design does instead is make the theft *visible*
// and *bounded*: the launch cookie is single use, so the page's own `POST`
// then fails and the desk says "No session — open the URL that jpack-desk
// printed at startup" rather than working while somebody else is also inside.
// Sixty seconds, one use, and a failure the person sees. That is the whole of
// the claim.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// launchCookiePrefix names the one cookie this chassis sets, and the port is
// appended: a cookie's origin has no port, so two desks on one host would
// otherwise hand each other's browser tab the wrong handoff.
const launchCookiePrefix = "jpack-desk-launch"

// launchCookieName is the handoff cookie's name, port and all.
func launchCookieName(port int) string {
	return fmt.Sprintf("%s-%d", launchCookiePrefix, port)
}

// launchWindow is how long a handoff is good for.
//
// Long enough for a page to load and make one request on any machine anybody
// runs this on; short enough that a cookie sitting in a browser is not a
// standing key. It is not a session lifetime — the session it buys has none.
const launchWindow = 60 * time.Second

// bearerScheme is the credential scheme every authorization is presented under.
// One space, exactly: this is the whole grammar this desk reads.
const bearerScheme = "Bearer "

// fetchSiteHeader and fetchSiteSameOrigin are the browser's own account of
// where a request came from.
//
// **They gate exactly one route** — the exchange, which is the only one an
// ambient credential opens. The rest of this desk does not consult them, and
// must not: a script forges them freely, so they are a defence against a
// *page*, and only where a page could otherwise drive somebody's cookie.
const (
	fetchSiteHeader     = "Sec-Fetch-Site"
	fetchSiteSameOrigin = "same-origin"
)

// The WebSocket subprotocol offer that carries a session id on an upgrade.
//
// **A subprotocol rather than a query or a header**, and the reason is that the
// browser `WebSocket` constructor takes exactly two things: a URL and a list of
// subprotocols. It has no header parameter. A credential on the URL is the
// arrangement this desk spent two rounds removing, so the id goes in the one
// remaining place the page can put it deliberately — and the server answers
// with the plain `jpack-desk` protocol, so the id is offered and never echoed.
const (
	wsProtocol       = "jpack-desk"
	wsSessionPrefix  = "jpack-desk-session."
	wsProtocolHeader = "Sec-WebSocket-Protocol"
)

// maxSessions bounds the session store, evicted **least recently used**.
//
// A person with a desk open all day reopens the printed URL now and then, and
// each bootstrap mints a session that nothing else would ever remove. Sixty-four
// is far more open tabs than a local desk has. LRU rather than oldest-first
// because a session's age says nothing about whether a tab is still on screen:
// looking one up is evidence it is in use, and that is what `lookup` records.
const maxSessions = 64

// maxLaunches bounds the handoff store the same way. It is small because a
// handoff lives sixty seconds and is spent by the first page that loads.
const maxLaunches = 32

/* The two stores ------------------------------------------------------------- */

// session is one live session.
//
// `subject` and `issuer` are what `GET /api/session` answers with, and they are
// why this is a record rather than a set of ids: a session is the thing an
// identity provider fills in, and a bare set has nowhere to put the subject it
// authenticated. Today the exchange is the only thing that mints one, and what
// it writes is the local user and no issuer.
type session struct {
	subject string
	issuer  *string
	created time.Time
	// used is when this session was last presented, and it is what eviction
	// reads. A counter rather than a timestamp, so two lookups inside one clock
	// tick are still ordered.
	used uint64
}

// sessionStore is the set of live sessions, keyed by a **MAC of the id rather
// than the id itself**.
//
// # Why the key is a MAC
//
// A `map[string]session` keyed on the raw id would answer "is this id live?"
// with a hash-table probe over attacker-supplied bytes, and the whole reason
// the secret comparison in this package is `subtle.ConstantTimeCompare` is that
// a comparison whose cost depends on how much of a secret is right is a
// comparison that leaks the secret a byte at a time. A map lookup is exactly
// that shape: bucket selection and then key equality, short-circuiting.
//
// Keying on `HMAC-SHA256(processKey, id)` removes the signal rather than
// timing it away. The caller supplies the id; what is probed is its MAC under a
// key minted in this process and never disclosed, so no id an attacker can
// choose puts them nearer a live one, and the bucket a guess lands in tells
// them nothing about the bucket a real session is in. The MAC is computed over
// the whole id every time, at a cost that does not depend on the id's content.
type sessionStore struct {
	// key is this process's MAC key. Random, never written down, and gone when
	// the process is: sessions do not outlive the desk that minted them, so a
	// key that outlived it would only be a key to steal.
	key []byte

	mu sync.Mutex
	// live maps a session's MAC to the session. Nothing here is the id.
	live map[string]session
	// tick orders use. See `session.used`.
	tick uint64
}

func newSessionStore() (*sessionStore, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &sessionStore{key: key, live: make(map[string]session)}, nil
}

// handle is the map key for a session id: its MAC under this process's key.
//
// Exported to the package (rather than inlined at the call sites) so that the
// property is one function a reviewer can read and a test can name — see
// `TestSessionStoreHoldsNoSessionID` and `TestSessionHandleIsNotTheID`.
func (st *sessionStore) handle(id string) string {
	mac := hmac.New(sha256.New, st.key)
	// hash.Hash never returns an error, by contract.
	_, _ = mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}

// create mints a session id and records the session behind it.
//
// The id is `NewToken`'s 192 bits, the same generator the launch secret comes
// from: a session id is a bearer credential for the whole of this desk's
// surface, so it is exactly as unguessable as the secret that bought it.
func (st *sessionStore) create(subject string, issuer *string) (string, error) {
	id, err := NewToken()
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.evictLocked()
	st.tick++
	st.live[st.handle(id)] = session{
		subject: subject, issuer: issuer, created: time.Now(), used: st.tick,
	}
	return id, nil
}

// evictLocked makes room for one more session, least recently used first.
//
// A loop rather than a single removal, so a store that somehow arrived over the
// bound comes back under it rather than staying one over for ever.
func (st *sessionStore) evictLocked() {
	for len(st.live) >= maxSessions {
		coldest, found := "", false
		var lowest uint64
		for key, held := range st.live {
			if !found || held.used < lowest {
				coldest, lowest, found = key, held.used, true
			}
		}
		if !found {
			return
		}
		delete(st.live, coldest)
	}
}

// lookup answers the session an id names, and **records the use**, which is
// what makes the bound above least-recently-used rather than oldest-first.
func (st *sessionStore) lookup(id string) (session, bool) {
	if id == "" {
		return session{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	key := st.handle(id)
	got, ok := st.live[key]
	if !ok {
		return session{}, false
	}
	st.tick++
	got.used = st.tick
	st.live[key] = got
	return got, true
}

// forget removes one session. This is sign-out.
func (st *sessionStore) forget(id string) bool {
	if id == "" {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	key := st.handle(id)
	if _, ok := st.live[key]; !ok {
		return false
	}
	delete(st.live, key)
	return true
}

// count is the number of live sessions, for a test.
func (st *sessionStore) count() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.live)
}

// launchStore holds the handoffs a launch has minted and not yet spent.
//
// Keyed by MAC for the same reason the session store is, and holding an expiry
// rather than a value: what a handoff is worth is "unspent, and recent".
type launchStore struct {
	key []byte
	// now is the clock, injectable so that expiry is a test rather than a wait.
	now func() time.Time

	mu    sync.Mutex
	given map[string]time.Time
}

func newLaunchStore() (*launchStore, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &launchStore{key: key, now: time.Now, given: make(map[string]time.Time)}, nil
}

func (ls *launchStore) handle(value string) string {
	mac := hmac.New(sha256.New, ls.key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// issue mints one handoff and records when it stops being good for anything.
func (ls *launchStore) issue() (string, error) {
	value, err := NewToken()
	if err != nil {
		return "", err
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.sweepLocked()
	ls.given[ls.handle(value)] = ls.now().Add(launchWindow)
	return value, nil
}

// consume spends one handoff, and it can only be spent once.
//
// **Removed rather than flagged.** A "used" flag is a second state to get
// wrong; a handoff that is gone cannot be spent twice by any code path,
// including one written later.
func (ls *launchStore) consume(value string) bool {
	if value == "" {
		return false
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	key := ls.handle(value)
	until, ok := ls.given[key]
	if !ok {
		return false
	}
	delete(ls.given, key)
	return !ls.now().After(until)
}

// sweepLocked drops handoffs nobody spent, and bounds the map.
func (ls *launchStore) sweepLocked() {
	now := ls.now()
	for key, until := range ls.given {
		if now.After(until) {
			delete(ls.given, key)
		}
	}
	for len(ls.given) >= maxLaunches {
		oldest, found := "", false
		var earliest time.Time
		for key, until := range ls.given {
			if !found || until.Before(earliest) {
				oldest, earliest, found = key, until, true
			}
		}
		if !found {
			return
		}
		delete(ls.given, oldest)
	}
}

func (ls *launchStore) count() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.given)
}

/* The handoff cookie ---------------------------------------------------------- */

// newLaunchCookie is the one cookie this chassis sets, and the one place its
// attributes are written.
//
// Every attribute is a decision:
//
//   - the **name carries the port**, because a cookie's origin does not.
//   - `Max-Age=60` — a handoff is not a session and must not outlive the page
//     load it exists for. It is the difference between a cookie that is a
//     window and a cookie that is a key.
//   - `HttpOnly` — page code cannot read it, so it cannot put it somewhere a
//     URL goes. The page does not need to: it sends the cookie by making one
//     same-origin request, and gets a bearer id back.
//   - `SameSite=Strict` — no request another site initiated carries it.
//   - `Path=/` — the exchange is under `/api`, the launch is at `/`, and a
//     narrower path would simply mean a second cookie.
//   - `Secure` only over https — this chassis binds loopback and serves plain
//     http, and a browser discards a `Secure` cookie that arrives over http, so
//     setting it unconditionally would mean setting no cookie at all.
func newLaunchCookie(name, value string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(launchWindow / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	}
}

// expireLaunchCookie is the same cookie, spent.
func expireLaunchCookie(name string, secure bool) *http.Cookie {
	spent := newLaunchCookie(name, "", secure)
	spent.MaxAge = -1
	return spent
}

/* The ways in ----------------------------------------------------------------- */

// bearerOf is the credential presented as `Authorization: Bearer …`, or "".
func bearerOf(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerScheme) || !strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return ""
	}
	return header[len(bearerScheme):]
}

// offeredSessionID is the session id a WebSocket upgrade offered as a
// subprotocol, or "".
//
// The browser sends the offer as one comma-separated header (or several, which
// `Header.Values` gives us separately), so both shapes are read. Whitespace
// around a comma is the grammar's, not the page's.
func offeredSessionID(r *http.Request) string {
	for _, header := range r.Header.Values(wsProtocolHeader) {
		for _, offer := range strings.Split(header, ",") {
			offer = strings.TrimSpace(offer)
			if id, ok := strings.CutPrefix(offer, wsSessionPrefix); ok {
				return id
			}
		}
	}
	return ""
}

// launchSecretPresented reports whether the request carries the launch secret.
//
// This is the script's door — `scripts/containment-check.sh`, the smoke client,
// the acceptance run, and every test in this package. It is compared in
// constant time, and it is compared against the **launch secret only**: a
// session id presented here is looked up in the store instead, one line up in
// `authorized`, so the two credentials never stand in for each other.
func (s *Server) launchSecretPresented(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(bearerOf(r)), []byte(s.cfg.Token)) == 1
}

// sessionOf is the session this request presents, where it presents one.
//
// **No cookie is consulted.** A cookie authorizes exactly one route on this
// desk — `POST /api/session`, which spends it — and nothing else, ever. That is
// the whole point of the shape: a credential the browser attaches by itself is
// a credential every local service and every replaying script gets to use.
func (s *Server) sessionOf(r *http.Request) (session, bool) {
	if id := bearerOf(r); id != "" {
		if held, ok := s.sessions.lookup(id); ok {
			return held, true
		}
	}
	if id := offeredSessionID(r); id != "" {
		return s.sessions.lookup(id)
	}
	return session{}, false
}

/* The bootstrap --------------------------------------------------------------- */

// handleLaunch is the one path the launch secret is ever sent to, and all it
// does is trade it for a sixty-second, single-use handoff.
//
// **It mints no session.** A launch that minted one would be a launch whose
// answer is a standing credential in a cookie jar, which is the arrangement
// this replaces. What it sets is a cookie that is worth exactly one call to
// `POST /api/session` and nothing else.
//
// **The redirect is the point.** Answering `303 See Other` to `/#` means the
// browser's address bar holds `/` and not the secret. The explicit empty
// fragment is load-bearing: a `Location` carrying none inherits the *request's*
// (RFC 9110 §10.2.2), so `/launch?secret=S#S` would land on `/#S` and leave the
// secret in `location.hash`.
//
// **A wrong secret sets nothing**, and says so in one line. There is nothing to
// distinguish "absent" from "wrong" — both are `403`, because a caller that
// could tell them apart would have an oracle for the shape of the secret.
//
// **No Origin guard stands here, deliberately.** Every gated route has one, and
// this route is not gated: it is where authorization is *acquired*. A
// cross-site request to it can only succeed by already holding the secret, and
// the cookie it sets is `SameSite=Strict` and spent by one request. What the
// guard *would* refuse is the ordinary case: a person pasting the printed URL
// into a fresh tab sends no `Origin` at all.
func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	// A response that hands out a credential is never a cached response.
	w.Header().Set("Cache-Control", "no-store")
	secret := r.URL.Query().Get("secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.Token)) != 1 {
		http.Error(w, "the launch secret is missing or wrong: open the URL jpack-desk printed at startup", http.StatusForbidden)
		return
	}
	handoff, err := s.launches.issue()
	if err != nil {
		http.Error(w, "this desk could not begin a session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, newLaunchCookie(s.launchCookie, handoff, requestScheme(r) == "https"))
	http.Redirect(w, r, "/#", http.StatusSeeOther)
}

// handleLaunchSubpath answers everything under `/launch/`.
//
// **404 from the mux, never the SPA fallback.** `GET /launch` matches one exact
// path, so `/launch/anything?secret=…` fell through to `handleStatic`, which
// treats an unknown extensionless path as a client-side route and serves the
// page — with the secret still on the URL, in the history and in every
// `Referer` that page goes on to send. Nothing is under `/launch/`, and this
// says so. `handleStatic` refuses the same shapes again, for the spellings the
// router does not see as this route: another case, or a secret on some other
// path's query.
func (s *Server) handleLaunchSubpath(w http.ResponseWriter, _ *http.Request) {
	refuseLaunchShape(w)
}

// refuseLaunchShape is the one answer for every near miss at the launch path.
func refuseLaunchShape(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "there is nothing under /launch", http.StatusNotFound)
}

// looksLikeALaunch reports whether a request must not be answered with the
// page, whatever else it looks like.
//
// Two shapes, and both are about **a secret ending up in the page's URL**:
//
//   - any path at or under `launch`, in any case. The router already owns
//     `/launch` and `/launch/…` exactly; this catches `/Launch/x` and the
//     spellings a percent-encoded separator produces once `net/http` has
//     normalised them, which reach the static handler instead.
//   - any request whose query carries a `secret` pair at all. If a URL like
//     that is ever loaded as a page, the secret is in `window.location`, in
//     history and in `Referer` — and no page on this desk has ever needed one.
func looksLikeALaunch(r *http.Request) bool {
	clean := strings.ToLower(strings.Trim(path.Clean(r.URL.Path), "/"))
	if clean == "launch" || strings.HasPrefix(clean, "launch/") {
		return true
	}
	// **Case-folded on the name.** `?SECRET=` is a different parameter to
	// `url.Query`, and nothing on this desk reads either — but "the page is
	// harmless so the spelling does not matter" is exactly the reasoning that
	// put a secret in an address bar twice. What matters is that a URL carrying
	// something called a secret is never answered with a page, whoever typed it.
	for name := range r.URL.Query() {
		if strings.EqualFold(name, "secret") {
			return true
		}
	}
	return false
}

/* The exchange ---------------------------------------------------------------- */

// handleSession is `GET`, `POST` and `DELETE` on one path.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createSession(w, r)
	case http.MethodDelete:
		s.deleteSession(w, r)
	default:
		s.readSession(w, r)
	}
}

// createSession spends a handoff and answers with a session id.
//
// # What it accepts, and why each check is here
//
//   - **The Origin guard**, as every gated route has it.
//   - **`Sec-Fetch-Site: same-origin`**, on this one request. It is CSRF
//     belt-and-braces beside `SameSite=Strict`: the launch cookie is ambient
//     for its sixty seconds, and this is the only route it opens, so this is
//     the only route where an ambient credential could be driven by a page that
//     is not ours. A browser cannot forge the header — it is a forbidden header
//     name — so no page can.
//   - **The launch cookie**, which is then gone.
//
// # The residual, stated here because this is where it lives
//
// **A script is not a browser, and forbidden-header rules bind browsers.** A
// script that captures the launch cookie inside its sixty-second window and
// forges `Sec-Fetch-Site: same-origin` can call this route and take the session
// before the page does. Nothing written here changes that; what the shape does
// is make it visible and bounded. The handoff is single use, so the page's own
// `POST` then fails and the desk says "No session — open the URL that jpack-desk
// printed at startup" rather than working while somebody else is also inside.
// A theft is a desk that stops working, not a desk that quietly has two users.
//
// A script that is *entitled* to a session does not need any of this: it
// presents the launch secret as `Authorization: Bearer` on this same route.
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r) {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden,
			fmt.Sprintf("origin %q is not permitted", r.Header.Get("Origin")))
		return
	}
	// The script's door, first: it needs no cookie and no fetch metadata.
	if s.launchSecretPresented(r) {
		s.mintSession(w)
		return
	}
	if r.Header.Get(fetchSiteHeader) != fetchSiteSameOrigin {
		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"a session is begun by this desk's own page, or by a script presenting the launch secret")
		return
	}
	cookie, err := r.Cookie(s.launchCookie)
	if err != nil || !s.launches.consume(cookie.Value) {
		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"no unspent launch is in progress: open the URL jpack-desk printed at startup")
		return
	}
	// Spent, and said so on the wire: the browser drops it here rather than
	// carrying a value that is already worthless for another fifty seconds.
	http.SetCookie(w, expireLaunchCookie(s.launchCookie, requestScheme(r) == "https"))
	s.mintSession(w)
}

func (s *Server) mintSession(w http.ResponseWriter) {
	id, err := s.sessions.create("local user", nil)
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			"this desk could not mint a session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "subject": "local user", "issuer": nil})
}

// readSession answers what this desk knows about the session the request
// presents: the subject, and the issuer that authenticated it.
//
// **Why it exists before anything renders it.** The session record is what an
// identity provider fills in — a sign-in writes a subject and an issuer here
// instead of the exchange's `local user` and `nil` — and an endpoint that
// reports the record is what makes it a thing rather than an internal detail.
func (s *Server) readSession(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	got, _ := s.sessionOf(r)
	writeJSON(w, http.StatusOK, map[string]any{"subject": got.subject, "issuer": got.issuer})
}

// deleteSession is sign-out, and it is the whole of it: the session is gone
// from this process and the id the page holds names nothing.
//
// There is still no *expiry* — a session nobody ends lives until the desk stops
// or the bound evicts it — and the README says so rather than letting this
// route imply otherwise.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	id := bearerOf(r)
	forgotten := s.sessions.forget(id)
	writeJSON(w, http.StatusOK, map[string]any{"forgotten": forgotten})
}
