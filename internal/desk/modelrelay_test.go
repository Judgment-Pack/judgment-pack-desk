package desk

// What these tests are for.
//
// The relay's claim is a custody claim and a containment claim at once: the
// page sends no credential and cannot be made to hold one; the desk sends
// exactly one, to exactly the endpoint on this machine's own `desk.json`, at a
// path the page may choose only from a closed character class. A claim like
// that is worth what its tests can discriminate, so every assertion below is
// made **at the upstream** — a recording endpoint that writes down what
// actually arrived — rather than against what this package believes it sent.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

/* Helpers ------------------------------------------------------------------ */

// arrival is one request as the endpoint received it.
type arrival struct {
	method   string
	path     string
	rawQuery string
	host     string
	header   http.Header
	body     []byte
	// What the endpoint was told about the body's length, and how it was
	// framed. Recorded because the relay measures a buffered body and declares
	// it: an endpoint that requires a declared length would refuse a chunked
	// request, and nothing else here would notice.
	contentLength    int64
	transferEncoding []string
}

// upstream is an endpoint that records what reached it.
//
// **The recording is the measurement.** Asserting that the handler deleted a
// header would be asserting that this package does what its own source says;
// what the promise is about is what a request looks like on the other side of
// the wire.
type upstream struct {
	mu      sync.Mutex
	seen    []arrival
	server  *httptest.Server
	respond http.HandlerFunc
}

func newUpstream(t *testing.T, respond http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{respond: respond}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.seen = append(u.seen, arrival{
			method:           r.Method,
			path:             r.URL.EscapedPath(),
			rawQuery:         r.URL.RawQuery,
			host:             r.Host,
			header:           r.Header.Clone(),
			body:             body,
			contentLength:    r.ContentLength,
			transferEncoding: append([]string(nil), r.TransferEncoding...),
		})
		u.mu.Unlock()
		if u.respond != nil {
			u.respond(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *upstream) arrivals() []arrival {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]arrival(nil), u.seen...)
}

func (u *upstream) only(t *testing.T) arrival {
	t.Helper()
	seen := u.arrivals()
	if len(seen) != 1 {
		t.Fatalf("the endpoint saw %d requests, want exactly 1", len(seen))
	}
	return seen[0]
}

// relayDesk is a desk configured for one endpoint, with a key stored.
func relayDesk(t *testing.T, kind string, u *upstream) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	return relayDeskAt(t, kind, u.server.URL)
}

func relayDeskAt(t *testing.T, kind, endpointURL string) (*Server, *httptest.Server, *bytes.Buffer) {
	t.Helper()
	s, ts, logged := assistantServer(t)
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":`+
			`{"url":%q,"kind":%q,"model":"a-model","tools":[]}}}`, endpointURL, kind))
	if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store: %d %v", status, body)
	}
	return s, ts, logged
}

// relayURL is one address on the relay, token and all.
//
// The token goes **first**, because the guard reads the first `token`
// parameter: a page that put one of its own before the desk's would be refused,
// which is the guard working and not the relay being tested.
func relayURL(ts *httptest.Server, suffix string) string {
	path, query, _ := strings.Cut(suffix, "?")
	address := ts.URL + relayPrefix + path + "?token=" + testToken
	if query != "" {
		address += "&" + query
	}
	return address
}

// relayGet sends one relayed request and reads the whole answer.
func relayGet(t *testing.T, ts *httptest.Server, suffix string) (*http.Response, string) {
	t.Helper()
	return relayDo(t, ts, http.MethodGet, suffix, nil, nil)
}

func relayDo(
	t *testing.T, ts *httptest.Server, method, suffix string, body io.Reader,
	decorate func(*http.Request),
) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, relayURL(ts, suffix), body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if decorate != nil {
		decorate(req)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, suffix, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

// codeOfBody is the refusal code a JSON envelope carries.
func codeOfBody(t *testing.T, body string) string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	code, _ := decoded["code"].(string)
	return code
}

// countingRelays swaps the relay's transport for the duration of a test, so
// that "no outbound request was made" is a claim about what left this process
// rather than about who happened to answer.
func countingRelays(t *testing.T) *countingTransport {
	t.Helper()
	counter := &countingTransport{}
	restore := relayTransport
	relayTransport = counter
	t.Cleanup(func() { relayTransport = restore })
	return counter
}

// unusableDesk is a desk whose configuration directory is not safe to keep a
// credential in — the one state in which this desk keeps no key at all.
func unusableDesk(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	loose := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(loose, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Group- and other-writable with no sticky bit: a directory in which
	// anybody may replace a name.
	if err := os.Chmod(loose, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	s, ts, _ := assistantServerIn(t, filepath.Join(loose, "jpack-desk"))
	return s, ts
}

// shortDeadlines makes the two bounds observable in a suite that finishes.
func shortDeadlines(t *testing.T, overall, idle time.Duration) {
	t.Helper()
	restoreOverall, restoreIdle := relayDeadline, relayIdle
	relayDeadline, relayIdle = overall, idle
	t.Cleanup(func() { relayDeadline, relayIdle = restoreOverall, restoreIdle })
}

/* The credential -----------------------------------------------------------*/

func TestRelayInjectsTheConfiguredKeyOncePerProtocol(t *testing.T) {
	for _, testCase := range []struct {
		kind   string
		header string
		want   string
	}{
		{"openai-compatible", "Authorization", "Bearer " + testKey},
		{"anthropic", "x-api-key", testKey},
		{"gemini", "x-goog-api-key", testKey},
	} {
		t.Run(testCase.kind, func(t *testing.T) {
			u := newUpstream(t, nil)
			_, ts, _ := relayDesk(t, testCase.kind, u)
			resp, _ := relayGet(t, ts, "models")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			seen := u.only(t)
			values := seen.header.Values(testCase.header)
			// Exactly one: a second copy is a header the page supplied that
			// survived beside the injected one, which is the failure that
			// would put a page-chosen credential on the wire.
			if len(values) != 1 || values[0] != testCase.want {
				t.Fatalf("%s = %v, want exactly one %q", testCase.header, values, testCase.want)
			}
		})
	}
}

// credentialHeaderCorpus is a fixed list of names a credential is carried in,
// written here and **read from nowhere**.
//
// The version of this test that came before iterated the production denylist to
// build its own inputs, which meant deleting a name from that list deleted the
// case that would have caught it: the test could only ever confirm that the
// loop ran. This corpus is independent of the implementation, and half of these
// names were never on that denylist at all.
var credentialHeaderCorpus = []string{
	"Authorization", "Proxy-Authorization", "Cookie",
	"X-Api-Key", "Api-Key", "X-Goog-Api-Key",
	"X-Auth-Token", "X-Access-Token", "X-Amz-Security-Token", "X-Session-Token",
	"Ocp-Apim-Subscription-Key", "X-Functions-Key", "Api-Secret", "X-Csrf-Token",
}

func TestRelayCarriesNoCredentialHeaderToTheEndpoint(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "anthropic", u)

	const smuggled = "sk-the-page-should-not-have-this"
	resp, _ := relayDo(t, ts, http.MethodGet, "v1/messages", nil, func(r *http.Request) {
		for _, header := range credentialHeaderCorpus {
			r.Header.Set(header, smuggled)
		}
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	injected, _, _ := credentialHeader("anthropic", testKey)
	for _, header := range credentialHeaderCorpus {
		if http.CanonicalHeaderKey(header) == http.CanonicalHeaderKey(injected) {
			// Replaced by the desk's own credential rather than merely
			// dropped; the assertion that the value is the desk's is below.
			continue
		}
		if got := seen.header.Values(header); len(got) != 0 {
			t.Errorf("%s reached the endpoint as %v", header, got)
		}
	}
	// And not under any spelling at all: the whole header set is searched for
	// the value the page sent.
	if strings.Contains(fmt.Sprint(seen.header), smuggled) {
		t.Errorf("what the page sent reached the endpoint: %v", seen.header)
	}
	// The one credential that did travel is the desk's.
	if got := seen.header.Get("x-api-key"); got != testKey {
		t.Errorf("x-api-key = %q, want the configured key", got)
	}
}

func TestRelayCarriesOnlyTheHeadersOnItsList(t *testing.T) {
	// **The structural half, and the reason the corpus above can never be the
	// whole story.** A denylist is a list of names somebody thought of; this
	// asserts the shape that makes the claim hold for a name nobody has
	// thought of yet — a header not on the allow-list does not travel, whatever
	// it is called and whatever it carries.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	unlisted := []string{
		"X-Something-The-Page-Set", "X-Request-Id", "Forwarded", "Referer",
		"X-Tomorrows-Credential", "Authorization-Info", "Cookie2",
	}
	resp, _ := relayDo(t, ts, http.MethodGet, "models", nil, func(r *http.Request) {
		for _, header := range unlisted {
			r.Header.Set(header, "sent-by-the-page")
		}
		// Origin is on the list of things that must not arrive, but it has to
		// be the one the guard accepts or this never reaches the relay at all.
		r.Header.Set("Origin", ts.URL)
	})
	unlisted = append(unlisted, "Origin")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	for _, header := range unlisted {
		if got := seen.header.Values(header); len(got) != 0 {
			t.Errorf("%s is not on the list and reached the endpoint as %v", header, got)
		}
	}
	// Whatever did arrive is on the list, plus the credential this desk adds
	// and the two the transport owns.
	own, _, _ := credentialHeader("openai-compatible", testKey)
	for header := range seen.header {
		switch http.CanonicalHeaderKey(header) {
		case http.CanonicalHeaderKey(own), "Host", "Content-Length":
			continue
		}
		if !relayedRequestHeader(header) {
			t.Errorf("%s arrived and is on no list", header)
		}
	}
}

func TestRelayStripsOriginAndReferer(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	// The Origin the page actually sends, which the guard accepts.
	origin := ts.URL
	resp, body := relayDo(t, ts, http.MethodGet, "models", nil, func(r *http.Request) {
		r.Header.Set("Origin", origin)
		r.Header.Set("Referer", origin+"/packs/one")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	if got := seen.header.Get("Origin"); got != "" {
		t.Errorf("Origin reached the endpoint as %q", got)
	}
	if got := seen.header.Get("Referer"); got != "" {
		t.Errorf("Referer reached the endpoint as %q", got)
	}
}

func TestRelayForwardsTheProtocolHeadersVerbatim(t *testing.T) {
	// The other half of the allow-list: what is on it arrives **unchanged**.
	// The relay adds a credential and rewrites nothing, so a page that sets
	// `anthropic-version` gets that version at the endpoint and not one this
	// desk decided was better.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "anthropic", u)
	sent := map[string]string{
		"Anthropic-Version":       "2023-06-01",
		"Anthropic-Beta":          "prompt-caching-2024-07-31",
		"Content-Type":            "application/json",
		"Accept":                  "text/event-stream",
		"X-Stainless-Lang":        "js",
		"X-Stainless-Retry-Count": "0",
	}
	resp, _ := relayDo(t, ts, http.MethodPost, "v1/messages",
		strings.NewReader(`{"model":"a-model"}`), func(r *http.Request) {
			for name, value := range sent {
				r.Header.Set(name, value)
			}
		})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	for name, value := range sent {
		if got := seen.header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestRelayStripsHopByHopHeaders(t *testing.T) {
	// Asserted rather than assumed. `httputil.ReverseProxy` does this, and
	// "the proxy does it" is a belief about a library until an endpoint has
	// been asked what it received.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, _ := relayDo(t, ts, http.MethodGet, "models", nil, func(r *http.Request) {
		r.Header.Set("X-Named-By-Connection", "1")
		r.Header.Set("Connection", "X-Named-By-Connection")
		r.Header.Set("Keep-Alive", "timeout=5")
		r.Header.Set("Proxy-Connection", "keep-alive")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	for _, header := range []string{
		"X-Named-By-Connection", "Keep-Alive", "Proxy-Connection",
	} {
		if got := seen.header.Values(header); len(got) != 0 {
			t.Errorf("hop-by-hop header %s reached the endpoint as %v", header, got)
		}
	}
}

func TestRelayDoesNotAnnounceTheDeskToTheEndpoint(t *testing.T) {
	// `Director` would have appended X-Forwarded-For; `Rewrite` does not, and
	// the Host is the endpoint's rather than this desk's loopback address.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, _ := relayDo(t, ts, http.MethodGet, "models", nil, func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "10.0.0.1")
		r.Header.Set("X-Forwarded-Host", "somewhere.example")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if got := seen.header.Values(header); len(got) != 0 {
			t.Errorf("%s reached the endpoint as %v", header, got)
		}
	}
	if seen.host != strings.TrimPrefix(u.server.URL, "http://") {
		t.Errorf("Host = %q, want the endpoint's own", seen.host)
	}
}

/* Method, body and address ------------------------------------------------- */

func TestRelayCarriesMethodAndBodyVerbatim(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	const payload = `{"model":"a-model","messages":[{"role":"user","content":"describe it"}]}`
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		resp, _ := relayDo(t, ts, method, "chat/completions", strings.NewReader(payload), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", method, resp.StatusCode)
		}
	}
	seen := u.arrivals()
	if len(seen) != 3 {
		t.Fatalf("%d requests reached the endpoint", len(seen))
	}
	for index, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if seen[index].method != method {
			t.Errorf("method %q, want %q", seen[index].method, method)
		}
		if string(seen[index].body) != payload {
			t.Errorf("body %q, want %q", seen[index].body, payload)
		}
	}
}

func TestRelayAppendsTheSuffixToTheConfiguredPath(t *testing.T) {
	u := newUpstream(t, nil)
	// A base with a path of its own, an escaped segment in it, and a query the
	// gateway routes on: all three are things a configured endpoint may carry
	// and all three have broken an address before.
	_, ts, _ := relayDeskAt(t, "openai-compatible", u.server.URL+"/tenant%2Fone/v1?route=eu")
	resp, _ := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	if seen.path != "/tenant%2Fone/v1/chat/completions" {
		t.Errorf("path %q — the configured escaping did not survive", seen.path)
	}
	// The configured query, and only that: nothing of the page's is forwarded,
	// the session token it necessarily sent included.
	if seen.rawQuery != "route=eu" {
		t.Errorf("query %q, want the configured one alone", seen.rawQuery)
	}
}

func TestRelayForwardsNothingOfThePagesQuery(t *testing.T) {
	// **Three reviewers leaked this desk's session token three different ways,
	// and each fix was a better comparison.** `?%74oken=` (the guard decodes
	// names and a raw compare did not), `?x=1;token=…&token=…` (Go rejects a
	// pair containing `;`; a server that still splits on one does not), and
	// `?Token=…&token=…` (this desk compared case-sensitively; ASP.NET Core's
	// query parser folds case). The class exists because the query was
	// forwarded at all: no comparison this desk can write is the comparison
	// every parser downstream makes.
	//
	// So a relayed request carries the session token and **nothing else**, and
	// every one of those spellings is now a 400 with nothing sent. The rule
	// this holds is the strongest one available: not "the token is removed"
	// but "there is nothing of the page's to remove".
	for _, testCase := range []struct {
		name     string
		query    string
		accepted bool
		status   int
		code     string
	}{
		{"the token alone", "token=" + testToken, true, http.StatusOK, ""},
		// Two of the same name: the guard reads the first, and the only name
		// present is still `token`.
		{"the token twice", "token=" + testToken + "&token=" + testToken, true, http.StatusOK, ""},
		// The case-folding leak, which is the finding this rule closes.
		{"a capitalised second name", "Token=" + testToken + "&token=" + testToken,
			false, http.StatusBadRequest, CodeAssistantRelayPath},
		{"an encoded capital", "%54oken=" + testToken + "&token=" + testToken,
			false, http.StatusBadRequest, CodeAssistantRelayPath},
		// And every ordinary parameter a page might reach for.
		{"a page parameter of its own", "token=" + testToken + "&x=1",
			false, http.StatusBadRequest, CodeAssistantRelayPath},
		// No token at all: refused by the guard, which comes first — a request
		// nothing authenticated never reaches the query rule.
		{"a page parameter alone", "x=1", false, http.StatusUnauthorized, CodeUnauthorized},
		{"an empty name", "token=" + testToken + "&=1",
			false, http.StatusBadRequest, CodeAssistantRelayPath},
		{"a name that will not decode", "token=" + testToken + "&%zz=2",
			false, http.StatusBadRequest, CodeAssistantRelayPath},
		{"a bare flag", "token=" + testToken + "&stream",
			false, http.StatusBadRequest, CodeAssistantRelayPath},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			counter := countingRelays(t)
			u := newUpstream(t, nil)
			_, ts, _ := relayDesk(t, "openai-compatible", u)
			resp, err := ts.Client().Get(ts.URL + relayPrefix + "models?" + testCase.query)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			body := string(raw)
			if !testCase.accepted {
				if resp.StatusCode != testCase.status {
					t.Fatalf("status %d, want %d: %s", resp.StatusCode, testCase.status, body)
				}
				if got := codeOfBody(t, body); got != testCase.code {
					t.Errorf("code %q, want %q", got, testCase.code)
				}
				if calls, to := counter.seen(); calls != 0 {
					t.Fatalf("a refused query made %d outbound request(s), to %v", calls, to)
				}
				return
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", resp.StatusCode, body)
			}
			// Accepted, and still nothing of the page's reaches the endpoint:
			// the token is not forwarded either.
			if calls, _ := counter.seen(); calls != 1 {
				t.Fatalf("%d outbound request(s), want 1", calls)
			}
			_, addresses := counter.seen()
			for _, address := range addresses {
				if strings.Contains(address, testToken) || strings.Contains(address, "token") {
					t.Errorf("the session token reached the endpoint: %q", address)
				}
			}
		})
	}
}

func TestRelayCarriesTheConfiguredQueryAndOnlyThat(t *testing.T) {
	// The positive control for the refusal above, and the half that has to keep
	// working: an endpoint's own routing is out of the file on this machine and
	// still travels — `?api-version=…` is how a whole vendor addresses its
	// models — while the token the page necessarily sent does not.
	u := newUpstream(t, nil)
	_, ts, _ := relayDeskAt(t, "openai-compatible",
		u.server.URL+"/v1?api-version=2024-10-21&route=eu%3Bwest")
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	// Byte for byte, escaping and order included. The escaped semicolon is the
	// case worth naming: it is a value and not a separator to anybody, so a
	// desk that refused it would be refusing an endpoint's own address.
	if seen.rawQuery != "api-version=2024-10-21&route=eu%3Bwest" {
		t.Errorf("query %q, want the configured one unchanged", seen.rawQuery)
	}
	if strings.Contains(seen.rawQuery, testToken) || strings.Contains(seen.rawQuery, "token") {
		t.Errorf("the page's token reached the endpoint: %q", seen.rawQuery)
	}
	if strings.Contains(fmt.Sprint(seen.header), testToken) {
		t.Errorf("the session token reached the endpoint in a header: %v", seen.header)
	}
}

func TestRelayRefusesAQueryCarryingASemicolon(t *testing.T) {
	// **Two parsers disagreed about one character and the desk's own session
	// token went to the endpoint.** `;` was a query separator once and some
	// servers still read it as one; Go does not, so
	// `x=1;token=<the token>&token=<the token>` reads to the guard as a single
	// `token` parameter — accepted — while the strip removed the pair it could
	// see and preserved `x=1;token=<the token>` byte for byte, for an endpoint
	// that does split on `;`.
	//
	// The answer is not a third parser: two readers can only be held to one
	// answer over the inputs they read the same way, so this query is refused.
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	for _, query := range []string{
		"x=1;token=" + testToken + "&token=" + testToken,
		"token=" + testToken + "&a=1;b=2",
		"token=" + testToken + ";",
	} {
		resp, body := relayDo(t, ts, http.MethodGet, "models?"+query, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400 (%s)", query, resp.StatusCode, body)
			continue
		}
		if got := codeOfBody(t, body); got != CodeAssistantRelayPath {
			t.Errorf("%q: code %q, want %q", query, got, CodeAssistantRelayPath)
		}
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("a semicolon query made %d outbound request(s), to %v", calls, to)
	}
	if seen := u.arrivals(); len(seen) != 0 {
		t.Fatalf("the endpoint saw %d request(s)", len(seen))
	}
}

func TestRelayAdmitsAMethodColonOnlyAsAClosedShape(t *testing.T) {
	// **The one exception to the segment class, tested as a shape.** The
	// native Gemini wire addresses a method with a colon in the last segment,
	// so `<name>:<method>` is accepted for a method on the closed list and
	// nothing else is — a colon before a name nobody wrote down would let
	// whoever holds the session token ask the configured endpoint to *do*
	// something, with the stored credential attached.
	for _, suffix := range []string{
		"v1beta/models/gemini-2.5-pro:generateContent",
		"v1beta/models/gemini-2.5-pro:streamGenerateContent",
		"v1beta/models/gemini-2.5-pro:countTokens",
		"v1beta/models/a_model-1.5:generateContent",
	} {
		if problem := relaySuffixProblem(suffix); problem != "" {
			t.Errorf("%q was refused: %s", suffix, problem)
		}
	}
	for _, testCase := range []struct{ name, suffix string }{
		{"a method nobody wrote down", "v1beta/models/m:deleteModel"},
		{"an empty method", "v1beta/models/m:"},
		{"an empty name", "v1beta/models/:generateContent"},
		{"a bare colon", "v1beta/models/:"},
		{"a second colon", "v1beta/models/a:b:generateContent"},
		{"a method with a tail", "v1beta/models/m:generateContent:x"},
		{"a case-folded method", "v1beta/models/m:GenerateContent"},
		// **Round 1's finding.** The rule was written per segment and never
		// asked where the segment was, so this was accepted and forwarded with
		// the credential — a resource nobody documented, under a verb this
		// desk agreed to. A method is a verb applied to the resource the path
		// names, so there is nothing after it.
		{"a method in a non-final segment", "v1beta/a:countTokens/b"},
		{"a method followed by anything at all", "v1beta/models/m:generateContent/x"},
		// The escaped spelling stays refused: no percent sign has ever been in
		// the class, which is what keeps the escaped and unescaped readings of
		// an accepted suffix the same string.
		{"an encoded colon", "v1beta/models/m%3AgenerateContent"},
		{"a colon in place of a separator", "v1beta:models:generateContent"},
	} {
		if problem := relaySuffixProblem(testCase.suffix); problem == "" {
			t.Errorf("%s: %q was accepted", testCase.name, testCase.suffix)
		}
	}
}

func TestRelayCarriesAMethodColonToTheEndpointByteForByte(t *testing.T) {
	// Through the whole server and measured at the upstream, because the
	// promise is about what the endpoint receives: a path this desk re-encoded
	// on the way through would be a request to a different resource with the
	// credential attached.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "gemini", u)
	const suffix = "v1beta/models/gemini-2.5-pro:streamGenerateContent"
	resp, body := relayGet(t, ts, suffix)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	if seen.path != "/"+suffix {
		t.Errorf("the endpoint saw %q, want %q", seen.path, "/"+suffix)
	}
	if got := seen.header.Values("x-goog-api-key"); len(got) != 1 || got[0] != testKey {
		t.Errorf("x-goog-api-key = %v, want exactly one configured key", got)
	}
}

func TestRelayRefusesAMethodColonWithoutReachingTheEndpoint(t *testing.T) {
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "gemini", u)
	for _, suffix := range []string{
		"v1beta/models/m:deleteModel",
		"v1beta/models/m:generateContent:x",
		"v1beta/models/:generateContent",
		// Round 1's non-final segment, through the whole server, so the
		// refusal is the route's and is counted at the transport.
		"v1beta/a:countTokens/b",
	} {
		resp, body := relayGet(t, ts, suffix)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400 (%s)", suffix, resp.StatusCode, body)
			continue
		}
		if got := codeOfBody(t, body); got != CodeAssistantRelayPath {
			t.Errorf("%q: code %q, want %q", suffix, got, CodeAssistantRelayPath)
		}
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("a refused method colon made %d outbound request(s), to %v", calls, to)
	}
}

func TestRelayAdmitsTheStreamPairOnGeminiAndOnNoOtherKind(t *testing.T) {
	// **The per-kind half of the query rule.** The pair is admitted for the
	// one wire that has nowhere else to ask for a stream, and the page's query
	// stays refused entirely for the other two — both of which carry streaming
	// in the request body, so a pair admitted for them would be a capability
	// nothing asked for.
	for _, kind := range AssistantKinds {
		t.Run(kind, func(t *testing.T) {
			u := newUpstream(t, nil)
			if kind != "gemini" {
				// Counted at the transport, because "the endpoint saw
				// nothing" is only a fact if nothing left this process.
				counter := countingRelays(t)
				_, ts, _ := relayDesk(t, kind, u)
				resp, body := relayGet(t, ts, "v1beta/models?alt=sse")
				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("status %d, want 400: %s", resp.StatusCode, body)
				}
				if got := codeOfBody(t, body); got != CodeAssistantRelayPath {
					t.Errorf("code %q, want %q", got, CodeAssistantRelayPath)
				}
				if calls, to := counter.seen(); calls != 0 {
					t.Fatalf("%d outbound request(s), to %v", calls, to)
				}
				return
			}
			// The accepted leg is measured at the endpoint itself: the
			// counting transport answers on its own behalf and would record a
			// request nobody could inspect the query of.
			_, ts, _ := relayDesk(t, kind, u)
			resp, body := relayGet(t, ts, "v1beta/models?alt=sse")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", resp.StatusCode, body)
			}
			seen := u.only(t)
			// Byte for byte, and the whole query: the token the page had to
			// send is not among it.
			if seen.rawQuery != "alt=sse" {
				t.Errorf("query %q, want exactly %q", seen.rawQuery, "alt=sse")
			}
			if strings.Contains(seen.rawQuery, "token") {
				t.Errorf("the session token reached the endpoint: %q", seen.rawQuery)
			}
		})
	}
}

func TestAConfiguredQueryCannotCarryWhatTheRelayReserves(t *testing.T) {
	// **Round 1's finding, and the reason it is a decode rule.** Only the
	// page's half of the query was checked; the configured half travelled
	// upstream byte for byte, and `PUT /api/desk-config` had just made that
	// half page-writable. A configured `?alt=sse` on gemini duplicated the one
	// pair the relay admits; on anthropic it sent a pair that kind admits none
	// of; and `?key=`, `?pageToken=` or a semicolon reached the endpoint on
	// every later call for as long as the file said so.
	//
	// The rule is now in both decoders, so a file carrying one of these is
	// refused **whole** — which means no endpoint is configured, and the relay
	// answers that rather than sending anything.
	for _, testCase := range []struct{ name, query string }{
		{"the pair the relay itself may add", "alt=sse"},
		{"the name the listing pages with", "pageToken=x"},
		{"a credential", "key=sk-nope"},
		{"a credential under another spelling", "access_token=nope"},
		{"a credential spelled auth", "auth=nope"},
		{"a semicolon", "a=1;b=2"},
		{"an encoded alias of a reserved name", "%61lt=sse"},
		// **Round 2.** The reserved names were compared case-sensitively, so
		// `?ALT=sse` was accepted and a streaming relay then added its own
		// pair beside it — an upstream that folds case sees two copies of one
		// name, which is exactly the disagreement these rules exist to keep
		// off the wire.
		{"a reserved name in another case", "ALT=sse"},
		{"a reserved name encoded in another case", "%41lt=sse"},
		{"the listing's name in another case", "PageToken=x"},
		// **Round 2.** Go read these as bytes and answered no error while the
		// browser's decoder throws, so the chassis accepted a file the page
		// refused — and could send the key on the strength of it.
		{"a name that is not UTF-8 once decoded", "%FF=x"},
		{"a value that is not UTF-8 once decoded", "a=%FF"},
		{"an overlong encoding", "a=%C0%AF"},
		{"a lone surrogate", "a=%ED%A0%80"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			counter := countingRelays(t)
			u := newUpstream(t, nil)
			s, ts, _ := assistantServer(t)
			// The key is stored against the same endpoint without the
			// offending query, so what is refused below is the query and not
			// the absence of a key.
			storeKeyBoundTo(t, s, ts, "gemini", u.server.URL+"/")
			configureEndpoint(t, s, "gemini", u.server.URL+"/?"+testCase.query)
			resp, body := relayGet(t, ts, "v1beta/models")
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
			}
			if got := codeOfBody(t, body); got != CodeAssistantUnconfigured {
				t.Errorf("code %q, want %q", got, CodeAssistantUnconfigured)
			}
			if calls, to := counter.seen(); calls != 0 {
				t.Fatalf("a refused configuration made %d outbound request(s), to %v", calls, to)
			}
			if seen := u.arrivals(); len(seen) != 0 {
				t.Fatalf("the endpoint saw %d request(s)", len(seen))
			}
		})
	}
	// The positive control: a configured query that is none of those still
	// travels, which is what makes this a rule rather than a ban.
	u := newUpstream(t, nil)
	_, ts, _ := relayDeskAt(t, "gemini", u.server.URL+"/?route=eu&api-version=2024-10-21")
	if resp, body := relayGet(t, ts, "v1beta/models"); resp.StatusCode != http.StatusOK {
		t.Fatalf("an ordinary configured query was refused: %d %s", resp.StatusCode, body)
	}
	if seen := u.only(t); seen.rawQuery != "route=eu&api-version=2024-10-21" {
		t.Errorf("query %q, want the configured one unchanged", seen.rawQuery)
	}
}

func TestTheConfiguredQueryRuleNamesEachClass(t *testing.T) {
	// The rule on its own, so it is exercised where the whole-file decode is
	// not the only route to it — and so the sentence a reader repairs the file
	// by is pinned.
	for _, testCase := range []struct{ query, want string }{
		{"key=x", "never stored in configuration"},
		{"apiKey=x", "never stored in configuration"},
		{"api_key=x", "never stored in configuration"},
		{"AUTH=x", "never stored in configuration"},
		{"secret=x", "never stored in configuration"},
		{"alt=sse", "the relay itself may add"},
		{"ALT=sse", "the relay itself may add"},
		{"Alt=sse", "the relay itself may add"},
		{"%41lt=sse", "the relay itself may add"},
		{"pageToken=x", "the relay itself may add"},
		{"PAGETOKEN=x", "the relay itself may add"},
		{"a=1;b=2", "semicolon"},
		{"%zz=1", "cannot read the same way a browser does"},
		// The half the rule used not to look at, and the three shapes Go read
		// as bytes while the browser threw.
		{"%FF=x", "cannot read the same way a browser does"},
		{"a=%FF", "cannot read the same way a browser does"},
		{"a=%C0%AF", "cannot read the same way a browser does"},
		{"a=%ED%A0%80", "cannot read the same way a browser does"},
		{"a=%zz", "cannot read the same way a browser does"},
	} {
		if got := endpointQueryProblem(testCase.query); !strings.Contains(got, testCase.want) {
			t.Errorf("%q refused with %q, want it to mention %q",
				testCase.query, got, testCase.want)
		}
	}
	// And an escape both sides read identically is still an escape, so this is
	// a rule about agreement rather than a ban on percent-encoding.
	for _, query := range []string{
		"", "route=eu", "api-version=2024-10-21&route=eu", "route=eu%3Bwest", "x=alt",
		"route=eu%E2%82%AC", "team=a%20b", "alternative=1", "a=b=c",
	} {
		if problem := endpointQueryProblem(query); problem != "" {
			t.Errorf("%q was refused: %s", query, problem)
		}
	}
}

func TestRelayRefusesEveryOtherSpellingOfTheStreamPair(t *testing.T) {
	// Byte equality against one fixed literal is the one comparison that has
	// no second reading. Each of these is a spelling some parser would fold
	// into `alt=sse`, and every one of them is refused with nothing sent.
	// The rule itself first, over every spelling — including the ones no
	// client can put on a request line, which the server below therefore
	// cannot exercise. A guard reachable by one route only is a guard that
	// stops being tested when that route changes.
	for _, query := range []string{
		"alt=json",
		"ALT=sse",
		"Alt=sse",
		"alt=SSE",
		"alt=sse&alt=sse",
		"%61lt=sse",
		"alt=sse&x=1",
		"alt",
		"alt=",
		"alt=sse ",
		"alt=sse&",
		"=sse",
	} {
		extra, problem := relayQueryProblem("token=" + testToken + "&" + query)
		if problem == "" {
			t.Errorf("%q was accepted, carrying %q", query, extra)
		}
	}
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "gemini", u)
	for _, query := range []string{
		"alt=json",
		"ALT=sse",
		"Alt=sse",
		"alt=SSE",
		"alt=sse&alt=sse",
		"%61lt=sse",
		"alt=sse&x=1",
		"alt",
		"alt=",
	} {
		resp, body := relayDo(t, ts, http.MethodGet, "v1beta/models?"+query, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400 (%s)", query, resp.StatusCode, body)
			continue
		}
		if got := codeOfBody(t, body); got != CodeAssistantRelayPath {
			t.Errorf("%q: code %q, want %q", query, got, CodeAssistantRelayPath)
		}
	}
	// The semicolon spelling is refused by the rule that came before this one,
	// and is checked here so that the exception cannot be read as reopening it.
	resp, body := relayDo(t, ts, http.MethodGet, "v1beta/models?alt=sse;x=1", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a semicolon: status %d, want 400 (%s)", resp.StatusCode, body)
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("a refused query made %d outbound request(s), to %v", calls, to)
	}
	if seen := u.arrivals(); len(seen) != 0 {
		t.Fatalf("the endpoint saw %d request(s)", len(seen))
	}
}

func TestRelayPutsTheStreamPairAfterTheConfiguredQuery(t *testing.T) {
	// The configured query is the endpoint's own routing and keeps its place;
	// the one pair the page may send goes after it. The order is `relayTarget`'s
	// and the probe's alike, which is why `appendQueryPair` is one function.
	u := newUpstream(t, nil)
	_, ts, _ := relayDeskAt(t, "gemini", u.server.URL+"/?route=eu%3Bwest")
	resp, body := relayDo(t, ts, http.MethodPost,
		"v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse",
		strings.NewReader(`{"contents":[]}`), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	if seen.rawQuery != "route=eu%3Bwest&alt=sse" {
		t.Errorf("query %q, want the configured one then the pair", seen.rawQuery)
	}
	if seen.path != "/v1beta/models/gemini-2.5-pro:streamGenerateContent" {
		t.Errorf("path %q", seen.path)
	}
	if seen.method != http.MethodPost || string(seen.body) != `{"contents":[]}` {
		t.Errorf("the endpoint saw %s with body %q", seen.method, seen.body)
	}
}

func TestRelayRefusesEverySuffixOutsideTheClass(t *testing.T) {
	// The rule itself, case by case. Two of these — an empty segment and a dot
	// segment written literally — never reach the handler through a mux that
	// cleans paths, which is why the rule is exercised here as well as through
	// the server below: a guard that is only reachable by one route is a guard
	// that stops being tested when that route changes.
	for _, testCase := range []struct{ name, suffix, want string }{
		{"empty", "", "at least one path segment"},
		{"empty segment", "chat//completions", "empty segment"},
		{"dot", "chat/./completions", `"." or ".."`},
		{"dot dot", "../secret", `"." or ".."`},
		{"trailing dot dot", "chat/..", `"." or ".."`},
		{"backslash", `chat\completions`, "only letters, digits"},
		{"percent", "%2e%2e/secret", "only letters, digits"},
		{"space", "chat completions", "only letters, digits"},
		// The scheme's colon introduces no method, so this meets the colon
		// rule before the character class — a different sentence, the same
		// refusal, and worth pinning so the exception cannot quietly widen.
		{"a whole URL", "https://elsewhere.example/v1", "a colon in a relayed path"},
		{"too long", strings.Repeat("a", maxRelaySuffix+1), "at most"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			problem := relaySuffixProblem(testCase.suffix)
			if problem == "" {
				t.Fatalf("%q was accepted", testCase.suffix)
			}
			if !strings.Contains(problem, testCase.want) {
				t.Errorf("%q refused with %q, want it to mention %q",
					testCase.suffix, problem, testCase.want)
			}
		})
	}
	// And the shapes both protocols actually use are accepted.
	for _, suffix := range []string{
		"models", "chat/completions", "v1/messages", "v1/models/a-model.v2", "openai/deployments/x_y-z",
	} {
		if problem := relaySuffixProblem(suffix); problem != "" {
			t.Errorf("%q was refused: %s", suffix, problem)
		}
	}
}

func TestRelayRefusesARefusedSuffixWithoutReachingTheEndpoint(t *testing.T) {
	// Through the whole server, so the refusal is the route's and not just the
	// function's — and counted at the transport, because "the endpoint saw
	// nothing" is only a fact if nothing left this process.
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	for _, suffix := range []string{"", "%2e%2e/secret", "a%2Fb", "chat%20x",
		strings.Repeat("a", maxRelaySuffix+1)} {
		resp, body := relayGet(t, ts, suffix)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400 (%s)", suffix, resp.StatusCode, body)
		}
		if got := codeOfBody(t, body); got != CodeAssistantRelayPath {
			t.Errorf("%q: code %q, want %q", suffix, got, CodeAssistantRelayPath)
		}
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("a refused suffix made %d outbound request(s), to %v", calls, to)
	}
}

func TestRelayRefusesABodyPastTheBound(t *testing.T) {
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, body := relayDo(t, ts, http.MethodPost, "chat/completions",
		strings.NewReader(strings.Repeat("x", maxRelayBody+1)), nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeTooLarge {
		t.Errorf("code %q, want %q", got, CodeTooLarge)
	}
	// **Refused, never truncated.** A body cut in half is a request the
	// endpoint answers about a document nobody wrote, so the measurement is
	// that nothing left this process at all.
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("an over-size body made %d outbound request(s)", calls)
	}
}

func TestRelayWeighsADeclaredBodyBeforeItReadsAnythingOfItsOwn(t *testing.T) {
	// **The fast rejection is a rejection, and this is what makes it visible.**
	// A declared length past the bound is refused before the store, the
	// endpoint or the key is read — so a desk that has none of those still
	// answers `too-large` rather than `assistant-unconfigured`. Without the
	// fast path the body would still be bounded at the reader, and every
	// assertion about the *status* would go on passing; what changes is which
	// refusal a desk with nothing configured gives, and that is the difference
	// this test is made of.
	counter := countingRelays(t)
	_, ts, _ := assistantServer(t)
	resp, body := relayDo(t, ts, http.MethodPost, "chat/completions",
		strings.NewReader(strings.Repeat("x", maxRelayBody+1)), nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeTooLarge {
		t.Fatalf("code %q, want %q — the size was weighed after the desk was read",
			got, CodeTooLarge)
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("an over-size body made %d outbound request(s)", calls)
	}
}

func TestRelayBoundsABodyOfUndeclaredLength(t *testing.T) {
	// The second half of the bound. A chunked body declares no length, so the
	// pre-check cannot see it and the read is what refuses it.
	//
	// **And nothing is sent, which is the half this used to miss.** The bound
	// used to be applied at the reader *while the proxy was already streaming
	// the body upstream*, so the endpoint received — and could act on — the
	// first eight mebibytes of a request this desk then refused, which is not
	// what "refused, never truncated" says. The whole body is read before a
	// byte of it is dispatched now, and the assertion is the same one the
	// declared-length case makes: zero outbound requests.
	counter := countingRelays(t)
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	// A reader with no Len, so net/http sends it chunked.
	unmeasured := io.LimitReader(neverEndingReader{}, maxRelayBody+1<<20)
	resp, body := relayDo(t, ts, http.MethodPost, "chat/completions", unmeasured, nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeTooLarge {
		t.Errorf("code %q, want %q", got, CodeTooLarge)
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("an over-size chunked body made %d outbound request(s), to %v", calls, to)
	}
	if seen := u.arrivals(); len(seen) != 0 {
		t.Fatalf("the endpoint received %d truncated request(s)", len(seen))
	}
}

func TestRelayCarriesABodyOfUndeclaredLengthWhenItFits(t *testing.T) {
	// The positive control for the buffering above: a chunked body inside the
	// bound still arrives, whole, with the length this desk measured.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	payload := strings.Repeat("x", 3<<20)
	unmeasured := io.LimitReader(neverEndingReader{}, int64(len(payload)))
	resp, body := relayDo(t, ts, http.MethodPost, "chat/completions", unmeasured, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	if len(seen.body) != len(payload) {
		t.Fatalf("the endpoint received %d bytes, want %d", len(seen.body), len(payload))
	}
	if string(seen.body) != payload {
		t.Error("the body that arrived is not the body that was sent")
	}
	// **And it arrives with the length this desk measured**, not as a chunked
	// stream. The relay buffers the body to bound it, so it knows the length
	// and declares it; an endpoint that requires one would refuse a chunked
	// request, and the body arriving intact would not have said so.
	if seen.contentLength != int64(len(payload)) {
		t.Errorf("the endpoint was told %d bytes, want %d", seen.contentLength, len(payload))
	}
	if len(seen.transferEncoding) != 0 {
		t.Errorf("the request was framed as %v, want a declared length",
			seen.transferEncoding)
	}
}

type neverEndingReader struct{}

func (neverEndingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

/* The answer --------------------------------------------------------------- */

func TestRelayForwardsTheAnswerVerbatimExceptSetCookie(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "abc")
		w.Header().Add("Set-Cookie", "session=theirs; Path=/")
		w.Header().Add("Set-Cookie", "another=one")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"error":"a teapot"}`))
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status %d, want the endpoint's 418", resp.StatusCode)
	}
	if body != `{"error":"a teapot"}` {
		t.Errorf("body %q, want the endpoint's own", body)
	}
	if got := resp.Header.Get("X-Request-Id"); got != "abc" {
		t.Errorf("X-Request-Id = %q, want the endpoint's", got)
	}
	// **The one response header that is dropped.** The page and this chassis
	// share an origin, so a cookie from the endpoint would be stored against
	// the desk and sent back to the desk's own endpoints.
	if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie reached the page as %v", got)
	}
}

func TestRelayFollowsNoRedirect(t *testing.T) {
	// Go strips `Authorization` across hosts and knows nothing about
	// `x-api-key`, so a followed redirect could walk the injected credential
	// to a host nobody configured.
	elsewhere := newUpstream(t, nil)
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", elsewhere.server.URL+"/v1/models")
		w.WriteHeader(http.StatusFound)
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	// The desk's own client must not follow it either, or the assertion below
	// would be about this test's client rather than about the relay.
	req, err := http.NewRequest(http.MethodGet, relayURL(ts, "models"), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	client := *ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want the endpoint's 302", resp.StatusCode)
	}
	if seen := elsewhere.arrivals(); len(seen) != 0 {
		t.Fatalf("the redirect was followed: %d request(s) reached the other host", len(seen))
	}
}

func TestRelayDeliversAStreamIncrementally(t *testing.T) {
	// **The assertion is an ordering, not a total.** A relay that buffered the
	// whole answer would still deliver both events and still pass a test that
	// only counted them; what says the stream is a stream is that the first
	// event is in the page's hands *before the endpoint has written the
	// second*.
	released := make(chan struct{})
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("the test endpoint cannot flush")
			return
		}
		_, _ = io.WriteString(w, "event: first\ndata: {}\n\n")
		flusher.Flush()
		<-released
		_, _ = io.WriteString(w, "event: second\ndata: {}\n\n")
		flusher.Flush()
	})
	_, ts, _ := relayDesk(t, "anthropic", u)

	req, err := http.NewRequest(http.MethodGet, relayURL(ts, "v1/messages"), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	first := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		if !strings.Contains(line, "first") {
			t.Fatalf("first line %q", line)
		}
	case <-time.After(5 * time.Second):
		close(released)
		t.Fatal("the first event never arrived while the endpoint was still writing")
	}
	close(released)
	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), "second") {
		t.Errorf("the rest of the stream never arrived: %q", rest)
	}
}

func TestRelayDeliversADeclaredLengthStreamIncrementally(t *testing.T) {
	// **This is the test that holds `FlushInterval: -1`, and the SSE one above
	// is not.** `httputil.ReverseProxy` flushes immediately on its own for a
	// `text/event-stream` body and for a body of unknown length, whatever the
	// field says — so the SSE case would go on passing with the field set to
	// zero, and a mutation row over it reported a safeguard nothing was
	// holding. What the field actually decides is this remaining case: an
	// answer that streams *and* declares its length, which is a shape an
	// endpoint is free to send and a page would otherwise wait out in full.
	released := make(chan struct{})
	const first, second = "the first half.", "the second half."
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", fmt.Sprint(len(first)+len(second)))
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("the test endpoint cannot flush")
			return
		}
		_, _ = io.WriteString(w, first)
		flusher.Flush()
		<-released
		_, _ = io.WriteString(w, second)
		flusher.Flush()
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)

	// **The client needs a deadline, and where it is needed is the Get.** A
	// buffered relay does not withhold the body — it withholds the *headers*,
	// because nothing has flushed yet and the answer declares its length, so
	// the request that a page would experience as "no answer" is the one this
	// test would experience as a suite that hangs.
	impatient := *ts.Client()
	impatient.Timeout = 5 * time.Second
	resp, err := impatient.Get(relayURL(ts, "chat/completions"))
	if err != nil {
		close(released)
		t.Fatalf("no answer arrived while the endpoint was still writing: %v", err)
	}
	defer resp.Body.Close()

	arrived := make(chan string, 1)
	go func() {
		buffer := make([]byte, len(first))
		read, _ := io.ReadFull(resp.Body, buffer)
		arrived <- string(buffer[:read])
	}()
	select {
	case got := <-arrived:
		if got != first {
			t.Fatalf("read %q, want %q", got, first)
		}
	case <-time.After(5 * time.Second):
		close(released)
		t.Fatal("the first half never arrived while the endpoint was still writing")
	}
	close(released)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != second {
		t.Errorf("the rest of the answer was %q, want %q", rest, second)
	}
}

/* The refusals ------------------------------------------------------------- */

func TestRelayRefusesWithoutTheSessionToken(t *testing.T) {
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, err := ts.Client().Get(ts.URL + relayPrefix + "chat/completions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, string(body)); got != CodeUnauthorized {
		t.Errorf("code %q, want %q", got, CodeUnauthorized)
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("an unauthorized request made %d outbound request(s)", calls)
	}
}

func TestRelayRefusesAForeignOrigin(t *testing.T) {
	// The check that stops a page on another site driving this desk's key.
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, body := relayDo(t, ts, http.MethodGet, "models", nil, func(r *http.Request) {
		r.Header.Set("Origin", "https://elsewhere.example")
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeForbidden {
		t.Errorf("code %q, want %q", got, CodeForbidden)
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("a cross-origin request made %d outbound request(s)", calls)
	}
}

func TestRelayRefusesWithNoEndpointConfigured(t *testing.T) {
	counter := countingRelays(t)
	s, ts, _ := assistantServer(t)
	// A key that was stored, and then a file that names no endpoint. Storing
	// needs one, so the endpoint is configured, the key is bound to it, and
	// the configuration is then taken away — which is the state this case is
	// about and is now reachable only that way.
	storeKeyBoundTo(t, s, ts, defaultTestKind, defaultTestEndpoint)
	writeDeskConfig(t, s, `{"deskConfigVersion":1}`)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeAssistantUnconfigured {
		t.Errorf("code %q, want %q", got, CodeAssistantUnconfigured)
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("an unconfigured desk made %d outbound request(s)", calls)
	}
}

func TestRelayRefusesARefusedConfiguration(t *testing.T) {
	// **The whole-file contract, on this route too.** A `desk.json` the page
	// refuses must not authorise a relayed request any more than it authorises
	// a probe — and the transport rule is the case worth naming: an `http:`
	// endpoint off loopback is refused at decode, so it can never be reached
	// through here either.
	counter := countingRelays(t)
	s, ts, _ := assistantServer(t)
	// The key is stored against an endpoint this desk accepts, and the file is
	// then replaced by one it refuses — so the refusal under test is the
	// file's and not the key's absence.
	storeKeyBoundTo(t, s, ts, defaultTestKind, defaultTestEndpoint)
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":`+
		`{"url":"http://endpoint.example/v1","kind":"openai-compatible","model":"m","tools":[]}}}`)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeAssistantUnconfigured {
		t.Errorf("code %q, want %q", got, CodeAssistantUnconfigured)
	}
	if calls, to := counter.seen(); calls != 0 {
		t.Fatalf("a refused configuration made %d relayed request(s), to %v", calls, to)
	}
}

func TestRelayRefusesWithNoKeyStored(t *testing.T) {
	counter := countingRelays(t)
	u := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, fmt.Sprintf(
		`{"deskConfigVersion":1,"assistant":{"endpoint":`+
			`{"url":%q,"kind":"openai-compatible","model":"m","tools":[]}}}`, u.server.URL))
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeAssistantNoKey {
		t.Errorf("code %q, want %q", got, CodeAssistantNoKey)
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("a keyless desk made %d outbound request(s)", calls)
	}
}

func TestRelayRefusesWhereNoKeyCanBeKept(t *testing.T) {
	counter := countingRelays(t)
	_, ts := unusableDesk(t)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeAssistantUnusableStore {
		t.Errorf("code %q, want %q", got, CodeAssistantUnusableStore)
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("an unusable store made %d outbound request(s)", calls)
	}
}

func TestRelayRefusesPastTheConcurrencyBound(t *testing.T) {
	// A bound and not a queue: the answer past it is immediate and named.
	arrived := make(chan struct{}, maxRelayInFlight)
	released := make(chan struct{})
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-released
		w.WriteHeader(http.StatusOK)
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)

	var wg sync.WaitGroup
	for i := 0; i < maxRelayInFlight; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := ts.Client().Get(relayURL(ts, "chat/completions"))
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	for i := 0; i < maxRelayInFlight; i++ {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			close(released)
			wg.Wait()
			t.Fatalf("only %d of %d requests reached the endpoint", i, maxRelayInFlight)
		}
	}
	// **A bounded client, and the bound is load-bearing.** A relay that queued
	// instead of refusing would hold this request until the four ahead of it
	// finished — and an unbounded client would turn that into a suite that
	// hangs rather than a test that fails, which is the difference between a
	// safeguard that is checked and one that is only believed.
	impatient := *ts.Client()
	impatient.Timeout = 5 * time.Second
	resp, err := impatient.Get(relayURL(ts, "chat/completions"))
	if err != nil {
		close(released)
		wg.Wait()
		t.Fatalf("the request past the bound was queued rather than refused: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(raw)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503: %s", resp.StatusCode, body)
	} else if got := codeOfBody(t, body); got != CodeAssistantRelayBusy {
		t.Errorf("code %q, want %q", got, CodeAssistantRelayBusy)
	}
	close(released)
	wg.Wait()
	// And the bound is released: the same request works once the others are
	// done, which is what makes this a bound rather than a broken route.
	after, _ := relayGet(t, ts, "chat/completions")
	if after.StatusCode != http.StatusOK {
		t.Errorf("after the others finished: status %d", after.StatusCode)
	}
}

func TestRelayReportsAnUnreachableEndpointFromTheClosedVocabulary(t *testing.T) {
	u := newUpstream(t, nil)
	address := u.server.URL
	u.server.Close()
	_, ts, _ := relayDeskAt(t, "openai-compatible", address)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502: %s", resp.StatusCode, body)
	}
	if got := codeOfBody(t, body); got != CodeAssistantRelayUpstream {
		t.Errorf("code %q, want %q", got, CodeAssistantRelayUpstream)
	}
	if !strings.Contains(body, DiagnosticRefused) {
		t.Errorf("body %q, want the closed vocabulary's word for it", body)
	}
}

/* The two deadlines -------------------------------------------------------- */

func TestRelayDeadlinesDefaultToTenMinutesAndTwo(t *testing.T) {
	// The tests below shorten these, so the defaults are asserted here — a
	// bound nobody states is a bound that can be shortened to nothing by a
	// test and never noticed.
	if relayDeadline != 10*time.Minute {
		t.Errorf("relayDeadline is %v, want 10m", relayDeadline)
	}
	if relayIdle != 2*time.Minute {
		t.Errorf("relayIdle is %v, want 2m", relayIdle)
	}
}

func TestRelayBoundsOneRequestOverall(t *testing.T) {
	shortDeadlines(t, 150*time.Millisecond, time.Minute)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, DiagnosticTimeout) {
		t.Errorf("body %q, want the vocabulary's word for a timeout", body)
	}
}

func TestRelayBoundsTheGapBetweenWrites(t *testing.T) {
	shortDeadlines(t, time.Minute, 150*time.Millisecond)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: first\ndata: {}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// And then nothing at all, which is the case the overall deadline
		// cannot see quickly and the page experiences as an answer that never
		// finishes.
		select {
		case <-stop:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	_, ts, _ := relayDesk(t, "anthropic", u)
	resp, err := ts.Client().Get(relayURL(ts, "v1/messages"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d — the answer had already begun", resp.StatusCode)
	}
	read, err := io.ReadAll(resp.Body)
	if !strings.Contains(string(read), "first") {
		t.Errorf("what did arrive was %q", read)
	}
	// The stream is cut rather than left open for ever: what a stalled answer
	// must not do is hold the page until something else gives up.
	if err == nil {
		t.Error("a stalled stream ended cleanly, so nothing bounded it")
	}
}

func TestRelayReleasesASlotHeldByAClientThatStoppedReading(t *testing.T) {
	// **The bound was on the wrong half of the request.** The two deadlines
	// cancel the *upstream* context; neither ends a write to a client that has
	// stopped reading, and the desk's server has no `WriteTimeout` on purpose
	// (`/ws` is a socket it holds open for a session). So four clients that
	// authenticated and then stopped reading could hold all four slots for
	// ever, and every later request answered `assistant-relay-busy` about a
	// desk that was carrying nothing anybody was waiting for.
	//
	// Raw connections rather than a client, because the property is "this
	// socket is never read from again" and every HTTP client in the standard
	// library reads.
	// **Two arrangements, because two different bounds have to release the
	// slot.** With a short idle bound it is the gap between writes that ends
	// the stalled write; with a short *overall* bound and an idle bound longer
	// than it, the cap is the only thing that can — and that second one is the
	// case the first version of this test could not see, which let a late write
	// take a fresh idle bound and hold a slot two minutes past the deadline
	// this desk advertises.
	for _, bounds := range []struct {
		name          string
		overall, idle time.Duration
	}{
		{"the idle bound releases it", 30 * time.Second, 300 * time.Millisecond},
		{"the overall bound caps it", 500 * time.Millisecond, 30 * time.Second},
	} {
		t.Run(bounds.name, func(t *testing.T) {
			relaySlotIsReleased(t, bounds.overall, bounds.idle)
		})
	}
}

func relaySlotIsReleased(t *testing.T, overall, idle time.Duration) {
	t.Helper()
	shortDeadlines(t, overall, idle)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	// Far more than any socket buffer, so the write to the stalled client
	// blocks rather than being absorbed.
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 128; i++ {
			select {
			case <-stop:
				return
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	address := strings.TrimPrefix(ts.URL, "http://")

	for i := 0; i < maxRelayInFlight; i++ {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { conn.Close() })
		// Written, and then never read from again.
		if _, err := fmt.Fprintf(conn, "GET %smodels?token=%s HTTP/1.1\r\nHost: %s\r\n"+
			"Connection: close\r\n\r\n", relayPrefix, testToken, address); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// All four in the endpoint's hands, so all four slots are taken.
	for len(u.arrivals()) < maxRelayInFlight {
		select {
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d requests reached the endpoint",
				len(u.arrivals()), maxRelayInFlight)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// **The assertion**: a fifth request succeeds once the stalled writes have
	// missed their deadline. Polled rather than timed exactly — what is being
	// held is that the slots come back, not when.
	impatient := *ts.Client()
	impatient.Timeout = 5 * time.Second
	// The bound this is entitled to wait: the overall deadline, plus the one
	// short write allowed past it, plus a second of slack. Anything longer is
	// a slot that is not coming back, and a suite that waits it out is one the
	// mutation harness reports as a timeout rather than as the caught mutation
	// it is.
	deadline := time.Now().Add(overall + relayFinalWrite + time.Second)
	var last int
	for time.Now().Before(deadline) {
		resp, err := impatient.Get(relayURL(ts, "models"))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		last = resp.StatusCode
		if last == http.StatusOK {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("every slot is still held by a client that stopped reading; the last answer was %d",
		last)
}

/* What is written down ----------------------------------------------------- */

func TestRelayLogsNeitherTheKeyNorTheAddress(t *testing.T) {
	// A configured URL carrying both a path and a query, because those are the
	// two parts of an address a log must not carry: some gateways route on a
	// query, and a routing value is somebody's deployment.
	//
	// **The query is a spelling this desk accepts**, and that is the point.
	// A credential-shaped name is refused at decode now — see
	// `TestAConfiguredQueryCannotCarryWhatTheRelayReserves` — so a test that
	// configured one would be asserting the log rule against a file the desk
	// never reads. What is held here is that the query it *does* read still
	// never reaches the log.
	u := newUpstream(t, nil)
	_, ts, logged := relayDeskAt(t, "openai-compatible",
		u.server.URL+"/gateway?route=eu-west-private")
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	// **Read after the handler has finished, not after the answer has
	// arrived.** The log line is written once `proxy.ServeHTTP` has returned,
	// which is *after* the page can have the whole body — so reading the
	// buffer here raced the handler, and did: the suite failed intermittently
	// on "nothing was logged" and read a buffer another goroutine was writing.
	// `Close` waits for every outstanding request, which is exactly the
	// happens-before this assertion needs.
	ts.Close()
	written := logged.String()
	// The event, and the origin, and nothing else. A log with nothing in it
	// would prove nothing about a handler that never ran, so the line is
	// required to be there.
	if !strings.Contains(written, "assistant relay") {
		t.Fatalf("nothing was logged about the relayed request: %q", written)
	}
	for _, forbidden := range []string{
		testKey, "eu-west-private", "/gateway", "chat/completions", "route",
	} {
		if strings.Contains(written, forbidden) {
			t.Errorf("the log carries %q: %s", forbidden, written)
		}
	}
}

func TestRelayNeverAnswersWithTheEndpointsOwnWords(t *testing.T) {
	// Every refusal this route produces is checked against the one thing it
	// must never contain. A transport failure has no body to quote, and the
	// rule that there is nothing to quote is what is held here rather than the
	// accident that today's error happens to be empty.
	//
	// **The endpoint's sentence carries no credential in it, and that is a
	// change.** It used to be `invalid key <the test key>` — which made this
	// test *require* the key to travel to the page, so the suite asserted the
	// hole rather than the property. What is being tested here is that the
	// endpoint's own answer travels; that it does not smuggle the key is
	// `TestRelayTakesTheKeyBackOutOfAnAnswer` below.
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	// A 401 from the endpoint is the endpoint's answer and travels whole —
	// this route forwards, it does not judge — so what is asserted is the
	// refusals *this desk* writes, below.
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(body, "invalid key") {
		t.Errorf("the endpoint's own answer did not travel: %q", body)
	}

	// And a refusal of this desk's own carries none of it.
	_, refused := relayGet(t, ts, "%2e%2e/secret")
	if strings.Contains(refused, "invalid key") {
		t.Errorf("a refusal quoted the endpoint: %q", refused)
	}
}

func TestRelayTakesTheKeyBackOutOfAnAnswer(t *testing.T) {
	// **An endpoint can hand the key back**, and the page must not receive it.
	// The credential this desk sends is the endpoint's own, so an endpoint that
	// echoes what it was sent — a debug gateway, a misconfigured proxy, a
	// hostile one — would otherwise put the machine-held key straight into the
	// browser, which is the single thing this route exists to prevent.
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		// Every conventional place a credential is echoed, and one nobody
		// named: the last is the case the name list cannot cover and the value
		// comparison must.
		w.Header().Set("Authorization", r.Header.Get("Authorization"))
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"x\"")
		w.Header().Set("X-Api-Key", testKey)
		w.Header().Set("Api-Key", testKey)
		w.Header().Set("X-Goog-Api-Key", testKey)
		w.Header().Set("X-Echo", testKey)
		w.Header().Set("X-Rate-Limit-Remaining", "42")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
	// **On the gemini kind**, so the `X-Goog-Api-Key` row of the strip list is
	// exercised against the endpoint whose credential header it actually is —
	// the case in which an echo would be the desk's own key coming back.
	_, ts, _ := relayDesk(t, "gemini", u)
	resp, body := relayGet(t, ts, "v1beta/models")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want the endpoint's 401", resp.StatusCode)
	}
	for header, values := range resp.Header {
		for _, value := range values {
			if strings.Contains(value, testKey) {
				t.Errorf("%s carried the key back to the page: %q", header, value)
			}
		}
	}
	for _, header := range reflectedCredentialHeaders {
		if got := resp.Header.Values(header); len(got) != 0 {
			t.Errorf("%s reached the page as %v", header, got)
		}
	}
	// A header under a name nobody listed, whose value **is** the key, is gone
	// on the value comparison alone.
	if got := resp.Header.Values("X-Echo"); len(got) != 0 {
		t.Errorf("X-Echo carried the key back as %v", got)
	}
	// And what is not a credential still travels: this route forwards an
	// answer, it does not censor one.
	if got := resp.Header.Get("X-Rate-Limit-Remaining"); got != "42" {
		t.Errorf("X-Rate-Limit-Remaining = %q, want the endpoint's own", got)
	}
	if body != `{"error":"unauthorized"}` {
		t.Errorf("body %q, want the endpoint's own", body)
	}
}

func TestRelayForwardsNoTrailer(t *testing.T) {
	// **The path `ModifyResponse` never sees.** `httputil.ReverseProxy` copies
	// the endpoint's trailers to the page *after* the body, past every filter
	// on this route — so an endpoint that announced `Trailer: X-Echo` and sent
	// the key in it handed over the credential under a name no list covers. No
	// trailer is forwarded at all now, and this reads the raw answer to say so:
	// a header map assertion alone would not see bytes on the wire.
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Echo, X-Api-Key")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "an answer")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		w.Header().Set("X-Echo", testKey)
		w.Header().Set("X-Api-Key", testKey)
		// The unannounced spelling too, which travels under a different rule.
		w.Header().Set(http.TrailerPrefix+"X-Late", testKey)
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)

	raw := rawRelayExchange(t, ts, "chat/completions")
	if !strings.Contains(raw, "an answer") {
		t.Fatalf("the endpoint's body did not arrive: %q", raw)
	}
	if strings.Contains(raw, testKey) {
		t.Errorf("the key reached the page in the raw answer:\n%s", raw)
	}
	for _, name := range []string{"X-Echo", "X-Api-Key", "X-Late", "Trailer"} {
		if strings.Contains(raw, name) {
			t.Errorf("%s reached the page:\n%s", name, raw)
		}
	}

	// And through a real client, whose trailer map is the other place a
	// trailer would show up.
	resp, err := ts.Client().Get(relayURL(ts, "chat/completions"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if len(resp.Trailer) != 0 {
		t.Errorf("the page received trailers: %v", resp.Trailer)
	}
}

func TestWithoutTrailersClearsWhatAProxyStages(t *testing.T) {
	// The second half of the rule, held on its own. `response.Trailer = nil`
	// stops the proxy copying anything; this is what clears whatever it staged
	// before this handler returns, which is the instant net/http would send it.
	header := http.Header{}
	header.Set("Trailer", "X-Echo")
	header.Set(http.TrailerPrefix+"X-Echo", testKey)
	header.Set(http.TrailerPrefix+"X-Api-Key", testKey)
	header.Set("Content-Type", "application/json")
	withoutTrailers(header)
	if got := header.Get("Trailer"); got != "" {
		t.Errorf("the announcement survived as %q", got)
	}
	for name := range header {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			t.Errorf("%s survived", name)
		}
	}
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("an ordinary header was removed: %q", got)
	}
}

func TestRelayForwardsNoInformationalResponse(t *testing.T) {
	// **The other path no filter sees.** `ReverseProxy` forwards a 1xx through
	// a client trace that copies its headers to the page and writes the status,
	// all before `ModifyResponse` runs — so an endpoint could put the key in a
	// `103 Early Hints` header and have it delivered. None is forwarded.
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Echo", testKey)
		w.Header().Set("Link", "</style.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Del("X-Echo")
		w.Header().Del("Link")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)

	raw := rawRelayExchange(t, ts, "chat/completions")
	if !strings.Contains(raw, `{"ok":true}`) {
		t.Fatalf("the endpoint's answer did not arrive: %q", raw)
	}
	if strings.Contains(raw, testKey) {
		t.Errorf("the key reached the page in a 1xx header:\n%s", raw)
	}
	if strings.Contains(raw, "103") || strings.Contains(raw, "Early") {
		t.Errorf("an informational response reached the page:\n%s", raw)
	}
}

// rawRelayExchange makes one relayed request on a socket of its own and returns
// every byte that came back.
//
// A raw connection because the properties above are about **bytes on the
// wire**: a trailer and a 1xx are both invisible to an assertion over a
// `*http.Response`'s header map, and a client that parses them away would hide
// exactly what is being asked about.
func rawRelayExchange(t *testing.T, ts *httptest.Server, suffix string) string {
	t.Helper()
	address := strings.TrimPrefix(ts.URL, "http://")
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET %s%s?token=%s HTTP/1.1\r\nHost: %s\r\n"+
		"Connection: close\r\n\r\n", relayPrefix, suffix, testToken, address); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	raw, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(raw)
}

func TestRelayTakesALongKeyOutOfAnAnswerWhereverItSits(t *testing.T) {
	// **The value rule has a length in it.** A key of twelve bytes or more is
	// looked for anywhere in a header's value, so `X-Echo: Bearer <key>` under
	// a name nobody listed goes as well as a bare echo. Below twelve it is
	// exact equality only: a three-character key is a substring of ordinary
	// text, and a filter that deletes the answer to protect a credential is a
	// worse answer than the credential.
	for _, testCase := range []struct {
		name     string
		key      string
		echoed   string
		survives bool
	}{
		{"a long key, echoed whole", "sk-long-enough-to-reason-about", "sk-long-enough-to-reason-about", false},
		{"a long key, inside a value", "sk-long-enough-to-reason-about", "Bearer sk-long-enough-to-reason-about", false},
		{"exactly twelve", "sk-012345678", "Bearer sk-012345678", false},
		{"one short of twelve, echoed whole", "sk-01234567", "sk-01234567", false},
		// Eleven bytes: exact equality only, so a value that merely contains
		// it stays. Stated as the behaviour it is rather than left to be met.
		{"one short of twelve, inside a value", "sk-01234567", "Bearer sk-01234567", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			echoed := testCase.echoed
			u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Echo", echoed)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			})
			s, ts, _ := assistantServer(t)
			writeDeskConfig(t, s, fmt.Sprintf(
				`{"deskConfigVersion":1,"assistant":{"endpoint":`+
					`{"url":%q,"kind":"openai-compatible","model":"m","tools":[]}}}`,
				u.server.URL))
			if status, body := storeKey(t, ts, testCase.key); status != http.StatusOK {
				t.Fatalf("store: %d %v", status, body)
			}
			resp, _ := relayGet(t, ts, "chat/completions")
			got := resp.Header.Get("X-Echo")
			if testCase.survives {
				if got != echoed {
					t.Errorf("X-Echo = %q, want %q — the short-key rule is exact equality",
						got, echoed)
				}
				return
			}
			if got != "" {
				t.Errorf("X-Echo carried %q back to the page", got)
			}
		})
	}
	// And the boundary itself, stated where a reader meets it.
	if minFingerprintable != 12 {
		t.Errorf("minFingerprintable is %d; the value rule's boundary moved with it",
			minFingerprintable)
	}
}

func TestRelayCannotTakeTheKeyOutOfABody(t *testing.T) {
	// **The residual, asserted so that it is a decision and not a surprise.**
	// The body is never read — the relay parses none of the traffic it carries,
	// a streamed answer cannot be scrubbed as it passes, and chunk 1 already
	// ruled that a *derived* representation of a credential cannot be detected
	// at all. So an endpoint that writes the key into its own body hands it to
	// the page, and the bound on that is not a filter: the key is the
	// endpoint's own credential and is good only at the endpoint that already
	// holds it. This test exists so that the limit is written down in the suite
	// as well as in the README.
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"echo":"` + testKey + `"}`))
	})
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	_, body := relayGet(t, ts, "chat/completions")
	if !strings.Contains(body, testKey) {
		t.Skip("the body is filtered after all; this test records a residual that no longer exists")
	}
}

/* Model listing ------------------------------------------------------------ */

func TestRelayCarriesEachProtocolsModelListing(t *testing.T) {
	// **The page's way of finding out what models an endpoint offers**, on all
	// three wires, through the relay that already exists — nothing on the
	// chassis is added for it. Each protocol's listing is a `GET` on its own
	// path under its own configured base, and what is asserted is what the
	// endpoint received: the desk's credential in that protocol's header
	// exactly once, and nothing at all of the page's.
	for _, testCase := range []struct {
		kind, base, suffix, path, header string
	}{
		{"openai-compatible", "/v1", "models", "/v1/models", "Authorization"},
		{"anthropic", "", "v1/models", "/v1/models", "x-api-key"},
		{"gemini", "", "v1beta/models", "/v1beta/models", "x-goog-api-key"},
	} {
		t.Run(testCase.kind, func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"listing":"the endpoint's own"}`))
			})
			_, ts, _ := relayDeskAt(t, testCase.kind, u.server.URL+testCase.base)
			const smuggled = "sk-the-page-should-not-have-this"
			resp, body := relayDo(t, ts, http.MethodGet, testCase.suffix, nil,
				func(r *http.Request) {
					for _, header := range credentialHeaderCorpus {
						r.Header.Set(header, smuggled)
					}
				})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			if body != `{"listing":"the endpoint's own"}` {
				t.Errorf("body %q, want the endpoint's own", body)
			}
			seen := u.only(t)
			if seen.method != http.MethodGet || seen.path != testCase.path {
				t.Errorf("the endpoint saw %s %s, want GET %s",
					seen.method, seen.path, testCase.path)
			}
			// The right credential, once, and the configured key rather than
			// anything the page sent.
			if got := seen.header.Values(testCase.header); len(got) != 1 ||
				!strings.Contains(got[0], testKey) {
				t.Errorf("%s = %v, want exactly one carrying the configured key",
					testCase.header, got)
			}
			// And nothing of the page's, under any spelling at all.
			if strings.Contains(fmt.Sprint(seen.header), smuggled) {
				t.Errorf("what the page sent reached the endpoint: %v", seen.header)
			}
			if strings.Contains(seen.rawQuery, "token") ||
				strings.Contains(fmt.Sprint(seen.header), testToken) {
				t.Errorf("the session token reached the endpoint: %q %v",
					seen.rawQuery, seen.header)
			}
		})
	}
}

func TestRelayRefusesTheListingsPaginationQuery(t *testing.T) {
	// **The listing this desk carries is first-page-only, and the reason is
	// the query rule.** Gemini's model listing pages with `pageToken`, and a
	// page cannot send one: nothing of the page's query is forwarded, and the
	// one exception is the literal `alt=sse`. That is a limit rather than an
	// oversight — see the README — and this is the assertion that it holds on
	// every kind, with nothing sent.
	for _, kind := range AssistantKinds {
		t.Run(kind, func(t *testing.T) {
			counter := countingRelays(t)
			u := newUpstream(t, nil)
			_, ts, _ := relayDesk(t, kind, u)
			for _, query := range []string{"pageToken=x", "pageSize=50", "pageToken=x&alt=sse"} {
				resp, body := relayDo(t, ts, http.MethodGet, "v1beta/models?"+query, nil, nil)
				if resp.StatusCode != http.StatusBadRequest {
					t.Errorf("%q: status %d, want 400 (%s)", query, resp.StatusCode, body)
					continue
				}
				if got := codeOfBody(t, body); got != CodeAssistantRelayPath {
					t.Errorf("%q: code %q, want %q", query, got, CodeAssistantRelayPath)
				}
			}
			if calls, to := counter.seen(); calls != 0 {
				t.Fatalf("a pagination query made %d outbound request(s), to %v", calls, to)
			}
			if seen := u.arrivals(); len(seen) != 0 {
				t.Fatalf("the endpoint saw %d request(s)", len(seen))
			}
		})
	}
}

/* The one table ------------------------------------------------------------ */

func TestTheProbeAndTheRelayPresentTheSameCredential(t *testing.T) {
	// Two callers, one table. A second table is how the two would come to
	// disagree about what an `anthropic` endpoint is sent, and the one that was
	// wrong would be wrong with a key in it.
	for _, kind := range AssistantKinds {
		probe, err := probeRequest(context.Background(),
			assistantEndpoint{url: "https://e.example/v1", kind: kind, model: "m"}, testKey)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		name, value, ok := credentialHeader(kind, testKey)
		if !ok {
			t.Fatalf("%s has no credential header", kind)
		}
		if got := probe.Header.Get(name); got != value {
			t.Errorf("%s: the probe sends %s=%q, the relay would send %q", kind, name, got, value)
		}
	}
	if _, _, ok := credentialHeader("something-else", testKey); ok {
		t.Error("a kind nothing defines was given a credential header")
	}
}
