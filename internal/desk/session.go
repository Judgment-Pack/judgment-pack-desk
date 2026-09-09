package desk

// The launch exchange and the session it mints.
//
// # Why the secret is exchanged rather than carried
//
// This chassis used to authenticate every request with `?token=<secret>`: the
// printed URL carried it, the page copied it out of `window.location` into
// `sessionStorage`, and every `fetch` and every WebSocket upgrade put it back
// on the query. A credential on a query is a credential in every place a URL
// goes — an address bar, a `Referer`, a proxy log, a browser's history, a
// screenshot, and `Response.url` inside the page itself — and the relay's own
// query rule (`relayQueryProblem`) exists only because a secret that travels
// that way has to be kept out of everything a URL is forwarded to.
//
// So the secret is **exchanged once, at one path, for a cookie**:
//
//   - `jpack-desk` prints `http://127.0.0.1:<port>/launch?secret=<secret>`;
//   - `GET /launch` compares the secret in constant time, mints a session id,
//     records it here, sets `jpack-desk-session` and answers `303 See Other`
//     to `/`;
//   - the browser lands on `/` with **no secret in the address bar**, and every
//     later request — `fetch`, upgrade, navigation — carries the cookie the
//     browser holds and nothing on its query.
//
// A script has no cookie jar worth the name, so it presents the launch secret
// as `Authorization: Bearer <secret>` instead. Those two are the whole of what
// `Server.authorized` accepts.
//
// # Why the secret is not single-use
//
// It stays valid for the life of the process. Single-use would make every
// closed tab a restart of the desk, which is a worse desk for no gain: the
// secret is already printed on the terminal of whoever started the process, and
// the property this chunk buys is not "the secret is spent" but **the secret
// never enters the page and never rides on a request query**. A person
// reopening the printed URL simply gets a second cookie.
//
// # Why a cookie is safe here, and what carries that
//
// A cookie is *ambient*, and worse than ambient: **cookies have no port
// isolation.** A cookie set for host `127.0.0.1` is sent to *every* port on
// that host, because the port is not part of a cookie's origin — it never has
// been. The `?token=` query this replaced had no such flaw, and two things are
// done about it here rather than left to the Origin guard alone:
//
//  1. **The cookie's name carries the port.** `jpack-desk-session-<port>`, from
//     the port the listener was bound to (`Config.Port`). Two desks on one
//     machine no longer overwrite each other's session, and a request that
//     reaches port 8791 is asked for *that* desk's cookie by name.
//  2. **A cookie authorizes only a request the browser itself calls
//     same-origin.** Two headers carry that claim, and which one is available
//     depends on the surface — see `Server.sameOriginClaim`:
//
//       - `Sec-Fetch-Site: same-origin` on everything `fetch` shaped. A page on
//         another port of the same host is *same-site*, not same-origin, so its
//         request says `same-site` and is refused even though the browser
//         attached the cookie to it.
//       - `Origin`, on the **WebSocket upgrade**, which carries no fetch
//         metadata at all. That is measured rather than assumed: Chrome 130
//         sends `Origin` and no `Sec-Fetch-*` on a handshake. `Origin` includes
//         the port, so a page on a sibling port names itself and is refused by
//         the same guard every other request meets.
//
//     Neither can be written by page code — both are forbidden header names —
//     and a request carrying **neither** is refused, which is what closes the
//     originless replay: a script that has somehow obtained the cookie cannot
//     produce a browser's account of itself, and a script that is entitled to
//     be here presents the launch secret instead.
//
// Beside those:
//
//   - `SameSite=Strict` — a request initiated by any other **site** does not
//     carry this cookie at all. Loopback ports are the same site, which is why
//     it is not sufficient on its own and why (2) exists.
//   - the Origin guard (`Server.originAllowed`), unchanged and kept on top:
//     it applies to every gated request, cookie or header alike.
//
// `HttpOnly` keeps the id out of `document.cookie`, so page code cannot read
// the session id and put it somewhere a URL goes — which is the mistake this
// whole change is undoing. `Secure` is set only where the request arrived over
// https: the chassis binds loopback and serves plain http, and a `Secure`
// cookie on an http response is a cookie the browser drops.
//
// **The compatibility cost is stated rather than hidden**: a browser that sends
// no `Sec-Fetch-Site` at all — Safari before 16.4 — cannot hold a session on
// this desk. Refusing is the right direction for a header whose absence must
// never be read as permission, and the README says so.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sessionCookiePrefix names the one cookie this chassis sets. The port is
// appended, because cookies are not isolated by port and two desks on one host
// would otherwise be one session — see the comment above.
const sessionCookiePrefix = "jpack-desk-session"

// sessionCookieName is the cookie this desk sets and reads, port and all.
func sessionCookieName(port int) string {
	return fmt.Sprintf("%s-%d", sessionCookiePrefix, port)
}

// fetchSiteHeader and fetchSiteSameOrigin are the browser's own account of
// where a request came from, on the surfaces that carry one.
//
// **Its absence is never permission by itself.** Where it is absent the claim
// has to come from `Origin` instead, and where both are absent there is no
// claim and a cookie authorizes nothing. See `Server.sameOriginClaim`.
const (
	fetchSiteHeader     = "Sec-Fetch-Site"
	fetchSiteSameOrigin = "same-origin"
)

// maxSessions bounds the store.
//
// The launch secret is valid for the life of the process and every exchange
// mints a session, so a person who reopens the printed URL all day would
// otherwise grow this map without bound. Sixty-four is far more open tabs than
// a local desk has and small enough that the whole store is trivial; eviction
// is oldest-first, so the tab someone is actually using is the last to go.
//
// **This is a bound, not an expiry.** Nothing here ends a session on time or on
// request; that arrives with sign-out.
const maxSessions = 64

// bearerScheme is the credential scheme a script presents the launch secret
// under. One space, exactly: this is the whole grammar this desk reads.
const bearerScheme = "Bearer "

// session is one browser session.
//
// `subject` and `issuer` are what `GET /api/session` answers with, and they are
// the reason this is a record rather than a set of ids: a session is the thing
// an identity provider fills in (7b), and a bare set has nowhere to put the
// subject it authenticated. Today the launch exchange is the only thing that
// mints one, and what it writes is the local user and no issuer.
type session struct {
	// subject is who this session is. `local user` for a launch exchange.
	subject string
	// issuer is the identity provider that authenticated the subject, and is
	// nil for every session this chunk mints — a launch secret asserts that
	// whoever ran the process holds it, and names no issuer. It is a pointer
	// rather than an empty string so that `GET /api/session` answers `null`
	// rather than `""`, which is the difference between "no provider" and "a
	// provider that named itself nothing".
	issuer *string
	// created is when the exchange happened. Nothing expires on it; it is
	// recorded because a session with no age is a session no later expiry rule
	// can be written against.
	created time.Time
	// seq is the order this session was minted in, and it is what eviction
	// reads. `created` would do on a clock with enough resolution, and two
	// sessions minted inside one tick would then be ties an eviction rule has
	// to break arbitrarily — a counter has no ties.
	seq uint64
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
//
// This is the "or by an HMAC of the id" half of the rule; the other half — a
// constant-time sweep over every live id — is O(n) per request for the same
// guarantee, and this desk holds one session per open tab.
type sessionStore struct {
	// key is this process's MAC key. Random, never written down, and gone when
	// the process is: sessions do not outlive the desk that minted them, so a
	// key that outlived it would only be a key to steal.
	key []byte

	mu sync.Mutex
	// live maps a session's MAC to the session. Nothing here is the id.
	live map[string]session
	// next is the sequence the next session takes. See `session.seq`.
	next uint64
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
// Exported to the package (rather than inlined at the two call sites) so that
// the property is one function a reviewer can read and a test can name — see
// `TestSessionStoreHoldsNoSessionID`, which asserts that no key of `live` is a
// session id, and `TestSessionHandleIsNotTheID`.
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
	st.next++
	st.live[st.handle(id)] = session{
		subject: subject, issuer: issuer, created: time.Now(), seq: st.next,
	}
	return id, nil
}

// evictLocked makes room for one more session, oldest first.
//
// A loop rather than a single removal, so a store that somehow arrived over the
// bound comes back under it rather than staying one over for ever.
func (st *sessionStore) evictLocked() {
	for len(st.live) >= maxSessions {
		oldest, found := "", false
		var lowest uint64
		for key, held := range st.live {
			if !found || held.seq < lowest {
				oldest, lowest, found = key, held.seq, true
			}
		}
		if !found {
			return
		}
		delete(st.live, oldest)
	}
}

// lookup answers the session an id names, and whether there is one.
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

// newSessionCookie is the cookie the launch exchange sets, and the one place
// its attributes are written.
//
// Every attribute is a decision:
//
//   - the **name carries the port**, because a cookie's origin does not — see
//     the comment at the top of this file.
//   - `Path=/` — the page, the file API, the relay and `/ws` are all under this
//     origin's root, and a narrower path would simply mean a second cookie.
//   - `HttpOnly` — page code cannot read the id, so it cannot put it back on a
//     query, in a log line, or into `sessionStorage`, which is the arrangement
//     this replaced.
//   - `SameSite=Strict` — the browser sends this cookie only on requests this
//     origin initiated. It is what makes an ambient credential safe to hold at
//     all, and it is why a gated request with **no** `Origin` header and only a
//     cookie is accepted: a foreign site cannot cause one.
//   - `Secure` only over https — this chassis binds loopback and serves plain
//     http, and a browser discards a `Secure` cookie that arrives over http, so
//     setting it unconditionally would mean setting no cookie at all.
//
// No `Expires` and no `Max-Age`: a session cookie dies with the browser
// session, which is the lifetime this desk actually has.
func newSessionCookie(name, id string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	}
}

/* The two ways in ------------------------------------------------------------ */

// sessionOf is the session this request's cookie names, where it names one.
// sameOriginClaim reports whether the browser has told us this request is this
// desk's own page talking to itself.
//
// **Two headers, because the two gated surfaces carry different ones.** This is
// measured rather than assumed:
//
//   - A `fetch` — every `/api/*` call the page makes — carries
//     `Sec-Fetch-Site`, and carries no `Origin` at all on a same-origin `GET`.
//   - A **WebSocket upgrade carries no `Sec-Fetch-*` header whatsoever** and
//     always carries `Origin`. Chrome 130 was measured doing exactly that; the
//     WebSocket protocol requires `Origin` of a browser client and says nothing
//     about fetch metadata.
//
// So each surface is judged by the signal it actually has, and neither signal
// is one page code can write — both are forbidden header names. A request with
// neither makes no claim, and a cookie on it authorizes nothing: that is the
// originless replay, and it is refused here rather than at the Origin guard,
// which accepts an absent `Origin` because a *script* legitimately sends none.
//
// The `Origin` branch is the ordinary guard, unchanged, so a page on a sibling
// port — which names its own port in `Origin` — is refused by the same
// comparison as any other foreign origin, and `--dev-token`'s allowance for the
// Vite dev server applies here too.
func (s *Server) sameOriginClaim(r *http.Request) bool {
	if site := r.Header.Get(fetchSiteHeader); site != "" {
		return site == fetchSiteSameOrigin
	}
	return r.Header.Get("Origin") != "" && s.originAllowed(r)
}

func (s *Server) sessionOf(r *http.Request) (session, bool) {
	// **The browser's own account of where this came from, first.** A cookie
	// reaches this desk on requests it must not authorize — every page on every
	// sibling port receives it, because cookies have no port isolation, and a
	// script can replay a stolen one by hand. Neither can produce the claim
	// above.
	if !s.sameOriginClaim(r) {
		return session{}, false
	}
	cookie, err := r.Cookie(s.cookieName)
	if err != nil || cookie.Value == "" {
		return session{}, false
	}
	return s.sessions.lookup(cookie.Value)
}

// launchSecretPresented reports whether the request carries the launch secret
// as `Authorization: Bearer <secret>`.
//
// This is the script's door — `scripts/containment-check.sh`, the smoke client,
// the acceptance run, and every test in this package. It is compared in
// constant time, and it is compared against the **launch secret only**: a
// session id presented here is not accepted, because the two are different
// credentials with different lifetimes and accepting either under one name
// would mean a leaked session id is a launch secret.
func (s *Server) launchSecretPresented(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerScheme) || !strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header[len(bearerScheme):]), []byte(s.cfg.Token)) == 1
}

/* The exchange ---------------------------------------------------------------- */

// handleLaunch is the one path the launch secret is ever sent to, and the only
// thing it does is trade it for a cookie.
//
// **The redirect is the point.** Answering `303 See Other` to `/` means the
// browser's address bar holds `/` and not the secret: nothing to leak through
// `Referer`, history, a screenshot or `window.location`. A `200` that rendered
// the page here would leave the secret in the URL, which is the arrangement
// this replaces.
//
// **A wrong secret sets nothing**, and says so in one line. There is no session
// to mint, no cookie to clear (a browser that already holds a good one keeps
// it; this request proves nothing about that one either way) and nothing to
// distinguish "absent" from "wrong" — both are `403`, because a caller that
// could tell them apart would have an oracle for the shape of the secret.
//
// **No Origin guard stands here, deliberately.** Every gated route has one, and
// this route is not gated: it is where authorization is *acquired*. A
// cross-site request to it can only succeed by already holding the secret, and
// a `SameSite=Strict` cookie set by one is not sent on any later cross-site
// request anyway — so the guard would refuse nothing an attacker could
// otherwise do. What it *would* refuse is the ordinary case: a person pasting
// the printed URL into a fresh tab sends no `Origin` at all on that navigation.
func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	// A response that hands out a credential is never a cached response.
	w.Header().Set("Cache-Control", "no-store")
	secret := r.URL.Query().Get("secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.Token)) != 1 {
		http.Error(w, "the launch secret is missing or wrong: open the URL jpack-desk printed at startup", http.StatusForbidden)
		return
	}
	id, err := s.sessions.create("local user", nil)
	if err != nil {
		http.Error(w, "this desk could not mint a session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, newSessionCookie(s.cookieName, id, requestScheme(r) == "https"))
	// **`/#`, with the empty fragment written out.** A redirect whose Location
	// carries no fragment inherits the *request's* — RFC 9110 §10.2.2 — so
	// `/launch?secret=S#S` would land on `/#S` and leave the secret sitting in
	// `location.hash`, which is exactly the leak this whole exchange exists to
	// close. An explicit empty fragment overrides it. The page then removes the
	// bare `#` from the address bar once, on load; see `main.tsx`.
	http.Redirect(w, r, "/#", http.StatusSeeOther)
}

// handleLaunchSubpath answers everything under `/launch/`.
//
// **404 from the mux, never the SPA fallback.** `GET /launch` matches one exact
// path, so `/launch/anything?secret=…` fell through to `handleStatic`, which
// treats an unknown extensionless path as a client-side route and serves the
// page — with the secret still on the URL, in the history and in every
// `Referer` that page goes on to send. Nothing is under `/launch/`, and this
// says so.
func (s *Server) handleLaunchSubpath(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "there is nothing under /launch/", http.StatusNotFound)
}

// handleSession answers what this desk knows about the session the request
// carries: the subject, and the issuer that authenticated it.
//
// **Why it exists before anything reads it.** The session record is what an
// identity provider fills in — 7b's sign-in writes a subject and an issuer here
// instead of the launch exchange's `local user` and `nil` — and an endpoint
// that reports the record is what makes it a thing rather than an internal
// detail. Nothing in the page renders it today: the identity slot remains
// display-only and reads the configured provider, exactly as before.
//
// **A cookie, and only a cookie.** It goes through the same guard as every
// other gated route, so a request holding the launch secret gets past the
// guard — and then finds it has no session to describe, because a script
// presenting a secret is not a browser holding a session. That is a `401` with
// the same code as any other missing authorization, and the sentence says which
// of the two it is.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	got, ok := s.sessionOf(r)
	if !ok {
		writeJSONCoded(w, http.StatusUnauthorized, CodeUnauthorized,
			"this endpoint describes a browser session, and this request carries none")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subject": got.subject, "issuer": got.issuer})
}
