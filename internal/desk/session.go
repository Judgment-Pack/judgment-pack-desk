package desk

// The bootstrap, the exchange, and the bearer session it produces.
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
// So the cookie is not the session. It is a **one-shot handoff**, and the shape
// is:
//
//  1. `GET /launch?secret=…` matches the launch secret in constant time and
//     sets `jpack-desk-launch-<port>`: a fresh 192-bit value, single use, good
//     for sixty seconds, `HttpOnly; SameSite=Strict; Path=/; Max-Age=60`. It
//     mints no session. Then `303` to `/#`.
//  2. The page loads and calls `POST /api/session` **once**. That request
//     consumes the handoff — it is removed from the store and cleared with
//     `Max-Age=-1` — and answers a 192-bit **session id** in the body.
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
// A script that captures the handoff **inside that window** and forges
// `Sec-Fetch-Site: same-origin` can take the session before the page does.
// Forbidden-header rules stop browsers, not scripts, and nothing this desk can
// write changes that. What the design does instead is make the theft *visible*
// and *bounded*: the handoff is single use, so the page's own `POST` then fails
// and the desk says "No session — open the URL that jpack-desk printed at
// startup" rather than working while somebody else is also inside. Sixty
// seconds, one use, and a failure the person sees. That is the whole of the
// claim.
//
// # What is deliberately absent
//
// There is **no renewal, no sign-out, no expiry, no eviction and no socket
// registry**. A session lives for the life of the process; the store refuses a
// 65th rather than making room by dropping one. Every one of those is a second
// actor that can end or replace a session while another is using it, and each
// pair of actors is a race. They arrive with the identity provider, each as its
// own PR, when there is a reason for them beyond symmetry.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
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
// with the plain `jpack-desk` protocol, so the id is never echoed *back*.
//
// **It is still on the request, and something supported does sit in between.**
// `Sec-WebSocket-Protocol` is a request header, so anything between the page
// and this process sees the id. In production that is nothing: the listener
// binds loopback. Under `npm run dev` it is **the Vite dev server**, which this
// repository documents and proxies `/ws` through — so in that configuration the
// dev server handles the session id, and it is written down here and in the
// README rather than left as an assumption about there being nothing in
// between. What answering with the plain protocol avoids is the id appearing in
// a *response* header as well, which is one more place for it to be kept.
const (
	wsProtocol       = "jpack-desk"
	wsSessionPrefix  = "jpack-desk-session."
	wsProtocolHeader = "Sec-WebSocket-Protocol"
)

// maxSessions bounds the session store, and the bound is a **refusal**.
//
// Sixty-four is far more open tabs than a local desk has, so reaching it is not
// an ordinary state — it is a page bootstrapping in a loop, or a script minting
// sessions it never uses. The two answers to that are to drop an old session or
// to refuse a new one, and this desk refuses.
//
// **Eviction was the other answer, and it was the wrong one here.** Whatever
// order it picks — oldest first, least recently used — it ends somebody's live
// session from the outside, so the page holding it has to notice, and noticing
// means a second actor in the page reading and replacing the credential the
// bootstrap owns. That is a race with every other actor, and this desk went
// through five review rounds finding those pairs one at a time. Refusing has
// one consequence, it is visible, and the sentence says what to do about it.
const maxSessions = 64

// errTooManySessions is that refusal, reported as a 503 rather than a 500: the
// desk is working and is temporarily unable to take another session.
var errTooManySessions = errors.New("this desk holds its maximum of sessions; restart it")

// maxLaunches bounds the handoff store. It is small because a handoff lives
// sixty seconds and is spent by the first page that loads.
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
	// minted counts every session this process has ever minted, and **never
	// goes down**: it is not `len(live)`, because nothing is ever removed and
	// because what it is for is a page noticing that a session appeared it did
	// not ask for.
	//
	// It is reported by `GET /api/session` and dies with the process, exactly
	// as the ids do. A page stores the count it saw beside its own id; a later
	// load that finds the count grown says so. That is what makes a theft
	// visible **after** the handoff's sixty seconds, when nothing about the
	// cookie can say anything any more.
	minted uint64
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
// A method rather than an inlined expression at the call sites, so that the
// property is one function a reviewer can read and a test can name — see
// `TestTheStoreHoldsNoSessionID`.
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
//
// **A full store refuses, and the check is under the lock that writes** — one
// check and not two. An earlier draft tested the bound before minting as well,
// to avoid generating an id it would throw away; two readings of one fact is
// exactly the shape this package spends its comments arguing against, and a
// mutation row proved the redundant one was holding nothing. Twenty-four bytes
// of entropy discarded on the one path that reaches the bound is not a cost.
func (st *sessionStore) create(subject string, issuer *string) (string, error) {
	id, err := NewToken()
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.live) >= maxSessions {
		return "", errTooManySessions
	}
	st.live[st.handle(id)] = session{subject: subject, issuer: issuer, created: time.Now()}
	st.minted++
	return id, nil
}

// lookup answers the session an id names.
//
// **It records nothing.** There is no recency here, because there is no
// eviction to order: a lookup is a read, and a read that wrote would be a
// second thing to get right about a store whose only rule is "64, then no".
func (st *sessionStore) lookup(id string) (session, bool) {
	if id == "" {
		return session{}, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	got, ok := st.live[st.handle(id)]
	return got, ok
}

// count is the number of live sessions, for a test.
func (st *sessionStore) count() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.live)
}

// mintedSoFar is what `GET /api/session` reports. See `sessionStore.minted`.
func (st *sessionStore) mintedSoFar() uint64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.minted
}

// handoffVerdict is what this desk knows about a handoff a request presented.
//
// **Four answers and not two, because a page acts differently on each.** The
// shape this replaces had one refusal for "no cookie" and "a cookie I do not
// hold", which meant a bogus cookie planted by anything on this host read as a
// theft — and a genuine theft, once the cookie was cleared, read as a reload.
type handoffVerdict int

const (
	// handoffUnknown is a value this desk never minted, or minted so long ago
	// that its record has gone. **It is ignored**: a page on a sibling loopback
	// port can set a cookie of this name at a longer path and the browser will
	// send it first, so a value nobody recognises must not be evidence of
	// anything. See `handoffPresented`.
	handoffUnknown handoffVerdict = iota
	// handoffAccepted is live, and spending it is what this verdict means.
	handoffAccepted
	// handoffSpent is one this desk finished with: taken by another caller, or
	// by this same tab on an earlier load.
	handoffSpent
	// handoffExpired is one this desk minted and let lapse.
	handoffExpired
)

// maxTombstones bounds how many finished handoffs this store remembers.
//
// **A ring, and the bound is the honest part.** Remembering that a handoff was
// spent is what lets the exchange tell a theft from a stranger's cookie, and
// remembering for ever would be a map that grows for the life of the process.
// Sixty-four is far more launches than a desk sees in a browser's lifetime for
// this cookie; past it the oldest record goes, and a value it named reads as
// unknown from then on.
//
// **What that costs, stated:** somebody holding the launch secret who takes a
// victim's handoff and then cycles sixty-four launches inside the victim's
// window pushes the spent record out, and the victim's next load reads
// `no-handoff` and keeps its id. The minted count still shows it — see
// `readSession` — which is why that count exists.
const maxTombstones = 64

// launchStore holds the handoffs a launch has minted and not yet spent, and a
// bounded record of the ones it has finished with.
//
// Keyed by MAC for the same reason the session store is, and holding an expiry
// rather than a value: what a handoff is worth is "unspent, and recent".
type launchStore struct {
	key []byte
	// now is the clock, injectable so that expiry is a test rather than a wait.
	now func() time.Time

	mu    sync.Mutex
	given map[string]time.Time
	// finished maps a handle to how this store finished with it.
	finished map[string]handoffVerdict
	// order is `finished`'s insertion order, so the ring evicts the oldest.
	order []string
}

func newLaunchStore() (*launchStore, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &launchStore{
		key:      key,
		now:      time.Now,
		given:    make(map[string]time.Time),
		finished: make(map[string]handoffVerdict),
	}, nil
}

// finishLocked records how this store finished with a handle, and keeps the
// record within the ring's bound.
func (ls *launchStore) finishLocked(key string, how handoffVerdict) {
	if _, already := ls.finished[key]; !already {
		ls.order = append(ls.order, key)
	}
	ls.finished[key] = how
	for len(ls.order) > maxTombstones {
		delete(ls.finished, ls.order[0])
		ls.order = ls.order[1:]
	}
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

// spend answers what this desk knows about one presented value, and spends it
// where it is live.
//
// **Removed from `given` rather than flagged.** A "used" flag is a second state
// to get wrong; a handoff that is gone cannot be spent twice by any code path,
// including one written later. What replaces the flag is a **tombstone**: the
// handle moves into `finished`, so a later presentation of the same value is
// `handoffSpent` rather than indistinguishable from a stranger's cookie.
func (ls *launchStore) spend(value string) handoffVerdict {
	if value == "" {
		return handoffUnknown
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	key := ls.handle(value)
	if until, ok := ls.given[key]; ok {
		delete(ls.given, key)
		if ls.now().After(until) {
			ls.finishLocked(key, handoffExpired)
			return handoffExpired
		}
		ls.finishLocked(key, handoffSpent)
		return handoffAccepted
	}
	if how, ok := ls.finished[key]; ok {
		return how
	}
	return handoffUnknown
}

// sweepLocked moves handoffs nobody spent into the ring, and bounds the map.
func (ls *launchStore) sweepLocked() {
	now := ls.now()
	for key, until := range ls.given {
		if now.After(until) {
			delete(ls.given, key)
			// **Tombstoned rather than forgotten**, so that a page loading
			// after its window says the link expired rather than saying
			// nothing at all.
			ls.finishLocked(key, handoffExpired)
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
		ls.finishLocked(oldest, handoffExpired)
	}
}

func (ls *launchStore) count() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.given)
}

// remembered is the size of the ring, for a test.
func (ls *launchStore) remembered() int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.finished)
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
//   - `SameSite=Strict` — no request another **site** initiated carries it.
//     Site is not origin: every port on `127.0.0.1` is the same site, so this
//     says nothing about a page on a sibling port. What bounds that is the
//     sixty seconds, the single use, and `Sec-Fetch-Site` on the one route this
//     cookie opens.
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
// subprotocol, or "" — and, where the offer is one this desk will not read, the
// sentence that says why.
//
// The browser sends the offer as one comma-separated header (or several, which
// `Header.Values` gives us separately), so both shapes are read. Whitespace
// around a comma is the grammar's, not the page's.
func offeredSessionID(r *http.Request) (string, string) {
	id, seen, plain := "", 0, false
	for _, header := range r.Header.Values(wsProtocolHeader) {
		for _, offer := range strings.Split(header, ",") {
			offer = strings.TrimSpace(offer)
			if offer == wsProtocol {
				plain = true
				continue
			}
			candidate, ok := strings.CutPrefix(offer, wsSessionPrefix)
			if !ok {
				continue
			}
			seen++
			id = candidate
		}
	}
	if seen == 0 && !plain {
		// No offer of ours at all: a script authorizing with the Bearer header,
		// or a request that is not from this desk's page. Not this rule's
		// business either way.
		return "", ""
	}
	if !plain {
		return "", "a desk upgrade offers the `jpack-desk` subprotocol; this one did not"
	}
	if seen > 1 {
		// Two ids is a request two readers could disagree about — the same
		// argument the relay's query rule makes — and there is no reading of it
		// this desk is willing to pick.
		return "", "a desk upgrade offers at most one session, and this one offered several"
	}
	if seen == 1 && !looksLikeASessionID(id) {
		return "", "the offered session is not the shape this desk mints"
	}
	return id, ""
}

// looksLikeASessionID is the shape `NewToken` produces: 48 lower-case hex
// characters.
//
// **Refused by shape before it is looked up**, so that a malformed offer is a
// `400` a caller can act on rather than a `401` that reads like a wrong
// credential — and so that nothing but hex is ever handed to the store.
func looksLikeASessionID(id string) bool {
	if len(id) != 48 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
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
//
// **And no subprotocol offer, either.** `/ws` reads its own, in
// `upgradeAuthorized`; a second path that authorized the same credential would
// be a second path to keep in step, and this one would be reachable only by a
// caller putting `Sec-WebSocket-Protocol` on a request that is not an upgrade.
func (s *Server) sessionOf(r *http.Request) (session, bool) {
	if id := bearerOf(r); id != "" {
		if held, ok := s.sessions.lookup(id); ok {
			return held, true
		}
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
	// **`GET` and nothing else, checked here rather than by the router.**
	// Registering `GET /launch` looks like it says this and does not: Go's mux
	// treats a `GET` pattern as matching `HEAD` too, so `HEAD /launch?secret=…`
	// minted a handoff and set the cookie — a credential handed out to a
	// request whose whole contract is that it has no body. Every other method
	// is a `405` that sets nothing.
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		refuseText(w, http.StatusMethodNotAllowed, CodeBadRequest,
			"the launch exchange answers GET, and only GET mints a handoff")
		return
	}
	secret := r.URL.Query().Get("secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.Token)) != 1 {
		refuseText(w, http.StatusForbidden, CodeForbidden,
			"the launch secret is missing or wrong: open the URL jpack-desk printed at startup")
		return
	}
	handoff, err := s.launches.issue()
	if err != nil {
		refuseText(w, http.StatusInternalServerError, CodeInternal,
			"this desk could not begin a session")
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
	refuseText(w, http.StatusNotFound, CodeNotFound, "there is nothing under /launch")
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
	return querySmellsOfASecret(r.URL.RawQuery)
}

// querySmellsOfASecret reports whether a raw query mentions a secret at all.
//
// **Read raw, and read as a substring, before anything parses it.** The first
// version of this asked `url.Query()`, which is a *parser* — and a parser has
// opinions. It drops a pair it cannot decode, so `?secret=<real>%ZZ` parsed to
// nothing and the page was served with the secret still on the URL; it splits
// on `&` and not `;`, so `?x=1;secret=<real>` was one parameter named `x` and
// went the same way. Every rule this desk has written against a query parser has
// lost to the next one — the relay's three leaks are the same story — and the
// lesson is to stop parsing.
//
// So: the raw bytes, case-folded, plus a **lenient** decode of them — every
// valid `%XX` resolved and every invalid one left alone — and the question is
// whether the substring `secret` appears. Lenient because `url.QueryUnescape`
// is all-or-nothing: one bad escape made it return nothing at all, so
// `?%73ecret=<secret>%ZZ` decoded to nothing and was answered with the page.
// That is deliberately blunt. It refuses `?mysecretpref=1`, and refusing a
// harmless page load is the cheap side of this trade; the expensive side is a
// secret in `window.location`, in history, and in every `Referer` the page then
// sends.
//
// **A fragment is not part of the query and is not seen here.** `#secret=…` is
// never sent to a server at all — the browser keeps it — so there is nothing on
// the wire to refuse, and the launch redirect's explicit empty fragment is what
// stops one being inherited into the page's address in the first place.
func querySmellsOfASecret(raw string) bool {
	if raw == "" {
		return false
	}
	if mentionsASecret(raw) || mentionsASecret(leniently(raw)) {
		return true
	}
	// Pair by pair as well as whole, because a separator can sit inside an
	// escape that only one of the two readings resolves.
	for _, piece := range strings.FieldsFunc(raw, func(r rune) bool { return r == '&' || r == ';' }) {
		if mentionsASecret(piece) || mentionsASecret(leniently(piece)) {
			return true
		}
	}
	return false
}

func mentionsASecret(s string) bool {
	return strings.Contains(strings.ToLower(s), "secret")
}

// leniently decodes every valid `%XX` in a string and leaves everything else
// exactly as it was.
//
// **`url.QueryUnescape` is all-or-nothing, and that was the hole.** One invalid
// escape anywhere makes it return an error and nothing else, so
// `?%73ecret=<secret>%ZZ` decoded to nothing, matched nothing, and was answered
// with the page — the secret then in `window.location`, in history and in every
// `Referer` that page sent. A single bad byte must not buy silence about the
// rest of the string.
//
// `+` is left as `+` rather than read as a space: this is not a form decoder,
// and turning `+` into a space could only ever create a match that the raw pass
// would have found anyway.
func leniently(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok := hexDigit(s[i+1]); ok {
				if lo, ok := hexDigit(s[i+2]); ok {
					out.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func hexDigit(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

/* The exchange ---------------------------------------------------------------- */

// handleSession is `GET` and `POST` on one path.
//
// **There is no `DELETE`.** Sign-out ends a session from outside the page that
// holds it, which means the page needs a second actor to notice — and this desk
// has exactly one, the bootstrap. It arrives with the identity provider, whose
// sign-out it will actually be, as its own PR.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.createSession(w, r)
		return
	}
	s.readSession(w, r)
}

// createSession spends a handoff and answers with a session id.
//
// # What it accepts, and why each check is here
//
//   - **The Origin guard**, as every gated route has it.
//   - **`Sec-Fetch-Site: same-origin`**, on this one request. It is CSRF
//     belt-and-braces beside `SameSite=Strict`: the handoff is ambient for its
//     sixty seconds, and this is the only route it opens, so this is the only
//     route where an ambient credential could be driven by a page that is not
//     ours. A browser cannot forge the header — it is a forbidden header name —
//     so no page can.
//   - **The handoff cookie**, which is then gone.
//
// # The residual, stated here because this is where it lives
//
// **A script is not a browser, and forbidden-header rules bind browsers.** A
// script that captures the handoff inside its sixty-second window and forges
// `Sec-Fetch-Site: same-origin` can call this route and take the session before
// the page does. Nothing written here changes that; what the shape does is
// bound it, and make it **visible**: the handoff is single use and this desk
// remembers that it was spent, so the page's own `POST` reads `handoff-spent`
// and the tab says *The launch link was used by something else. Restart
// jpack-desk and open the new URL it prints.* Reopening the printed URL is not
// the way back — the secret is reusable, so whatever took one handoff takes the
// next — and a restart is. Past the handoff's own lifetime the signal that
// remains is `sessions.minted` on `GET /api/session`; see `readSession`.
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
	// **Four answers, because a page acts differently on each**, and one of
	// them is silence. See `handoffVerdict` and `handoffPresented`.
	switch s.handoffPresented(r) {
	case handoffAccepted:
		// **Cleared here and nowhere else.** A refusal used to clear it too,
		// and that concealed a theft: a tab that reloaded after the browser
		// stored the clearing header but before the page had handled the
		// refusal presented nothing, read `no-handoff`, and kept its old
		// session. Only success clears; a stale one is bounded by its own
		// sixty-second `Max-Age` and reads as spent or expired until then.
		http.SetCookie(w, expireLaunchCookie(s.launchCookie, requestScheme(r) == "https"))
		s.mintSession(w)
	case handoffSpent:
		writeJSONCoded(w, http.StatusUnauthorized, CodeHandoffSpent,
			"this launch link was already used: restart jpack-desk and open the new URL it prints")
	case handoffExpired:
		writeJSONCoded(w, http.StatusUnauthorized, CodeHandoffExpired,
			"this launch link expired: open the URL jpack-desk printed at startup")
	default:
		writeJSONCoded(w, http.StatusUnauthorized, CodeNoHandoff,
			"no launch is in progress: open the URL jpack-desk printed at startup")
	}
}

// handoffPresented is what this request's cookies amount to.
//
// # Every cookie of the name, and unknown values ignored
//
// `r.Cookie` answers the **first** cookie of a name, and a cookie's identity
// includes a path this desk never sees. So a page on any other loopback port
// can set `jpack-desk-launch-<port>=bogus; Path=/api` — the browser sends the
// longer path first — and a rule that read one cookie and refused what it did
// not recognise would answer `handoff-spent` on every load, for ever, to a tab
// whose session is perfectly good. Restarting the desk on the same port would
// not repair it, because the planted cookie is still there.
//
// So: **every** cookie of the name is examined; the first that is live is
// spent and accepted; a value this desk finished with classifies the request
// where nothing better does; and a value nobody recognises is **ignored**,
// which reads as `no-handoff` and leaves the page's own id alone.
//
// An empty value is ignored for the same reason — `spend` answers unknown for
// it — and that also covers the desk's own cleared cookie arriving late.
func (s *Server) handoffPresented(r *http.Request) handoffVerdict {
	best := handoffUnknown
	for _, cookie := range r.Cookies() {
		if cookie.Name != s.launchCookie {
			continue
		}
		switch how := s.launches.spend(cookie.Value); how {
		case handoffAccepted:
			// **Spent under the store's own lock, and the answer is that
			// call's**, so two requests carrying one handoff cannot both be
			// told yes.
			return handoffAccepted
		case handoffSpent, handoffExpired:
			// The first classified answer stands. A second cookie may still be
			// live, which is why this does not return.
			if best == handoffUnknown {
				best = how
			}
		}
	}
	return best
}

// mintSession records a session and answers with its id.
//
// A store at its bound answers `503` and the sentence that says what to do
// about it, rather than `500`: nothing is broken, and a desk that said
// "internal error" would send a reader looking for one.
func (s *Server) mintSession(w http.ResponseWriter) {
	id, err := s.sessions.create("local user", nil)
	if errors.Is(err, errTooManySessions) {
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeSessionsFull, errTooManySessions.Error())
		return
	}
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
//
// It is also the one channel that tells a page whether the id it holds is still
// a session: a refused WebSocket upgrade reports no status to page code, so the
// page asks here instead. See `McpProvider`.
//
// **A script authorizing with the launch secret holds no session record**, and
// what it gets back says so: an empty subject and a null issuer, rather than an
// invented one. The secret is a way in, not somebody this desk authenticated.
func (s *Server) readSession(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	got, _ := s.sessionOf(r)
	// **`sessions.minted` is how a theft stays visible after the handoff is.**
	// Everything about a stolen handoff is over within sixty seconds; the count
	// of sessions this process has minted is not. A page stores the count it
	// saw beside its own id and says so on a later load if it grew — which is
	// the one signal that survives the ring in `launchStore` dropping a
	// tombstone, and the one a person meets hours later.
	//
	// It is a **count and not a list**: nothing here says anything about any
	// other session, and a page learning "one more than I knew about" is the
	// whole of what it is for.
	writeJSON(w, http.StatusOK, map[string]any{
		"subject":  got.subject,
		"issuer":   got.issuer,
		"sessions": map[string]any{"minted": s.sessions.mintedSoFar()},
	})
}
