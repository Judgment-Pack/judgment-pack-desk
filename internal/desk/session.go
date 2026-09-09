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
// A cookie is *ambient*: the browser attaches it to requests this desk's page
// did not make. Two things bound that, and both are load-bearing:
//
//   - `SameSite=Strict` — a request initiated by any other site does not carry
//     this cookie at all, so a foreign page cannot drive the runtime through
//     the browser that holds one;
//   - the Origin guard (`Server.originAllowed`), which is now this desk's CSRF
//     defence and applies to every gated request, cookie or header alike.
//
// `HttpOnly` keeps the id out of `document.cookie`, so page code cannot read
// the session id and put it somewhere a URL goes — which is the mistake this
// whole change is undoing. `Secure` is set only where the request arrived over
// https: the chassis binds loopback and serves plain http, and a `Secure`
// cookie on an http response is a cookie the browser drops.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sessionCookieName is the one cookie this chassis sets, and the only place a
// browser's authorization to this desk lives.
const sessionCookieName = "jpack-desk-session"

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
	// created is when the exchange happened. Nothing expires on it in this
	// chunk; it is recorded because a session with no age is a session no
	// later expiry rule can be written against.
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
	st.live[st.handle(id)] = session{subject: subject, issuer: issuer, created: time.Now()}
	return id, nil
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
func newSessionCookie(id string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	}
}

/* The two ways in ------------------------------------------------------------ */

// sessionOf is the session this request's cookie names, where it names one.
func (s *Server) sessionOf(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
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
	http.SetCookie(w, newSessionCookie(id, requestScheme(r) == "https"))
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
