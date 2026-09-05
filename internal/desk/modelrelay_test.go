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
			method:   r.Method,
			path:     r.URL.EscapedPath(),
			rawQuery: r.URL.RawQuery,
			host:     r.Host,
			header:   r.Header.Clone(),
			body:     body,
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

func TestRelayStripsEveryInboundCredentialHeaderByName(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "anthropic", u)

	const smuggled = "sk-the-page-should-not-have-this"
	resp, _ := relayDo(t, ts, http.MethodGet, "v1/messages", nil, func(r *http.Request) {
		for _, header := range inboundCredentialHeaders {
			r.Header.Set(header, smuggled)
		}
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	injected, _, _ := credentialHeader("anthropic", testKey)
	for _, header := range inboundCredentialHeaders {
		if http.CanonicalHeaderKey(header) == http.CanonicalHeaderKey(injected) {
			// Covered by the injection test above: this one is replaced by the
			// desk's own credential rather than merely deleted, and the
			// assertion that the value is the desk's is at the end of this test.
			continue
		}
		got := seen.header.Values(header)
		if len(got) != 0 {
			t.Errorf("%s reached the endpoint as %v", header, got)
		}
	}
	// And not by any spelling: the whole request is searched for the value the
	// page sent, so a header this list does not name still fails here.
	if strings.Contains(fmt.Sprint(seen.header), smuggled) {
		t.Errorf("what the page sent reached the endpoint: %v", seen.header)
	}
	// The one credential that did travel is the desk's.
	if got := seen.header.Get("x-api-key"); got != testKey {
		t.Errorf("x-api-key = %q, want the configured key", got)
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

func TestRelayForwardsEveryOtherHeaderVerbatim(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "anthropic", u)
	sent := map[string]string{
		"Anthropic-Version":        "2023-06-01",
		"Anthropic-Beta":           "prompt-caching-2024-07-31",
		"Content-Type":             "application/json",
		"Accept":                   "text/event-stream",
		"X-Something-The-Page-Set": "kept",
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
	resp, _ := relayGet(t, ts, "chat/completions?stream=true")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	seen := u.only(t)
	if seen.path != "/tenant%2Fone/v1/chat/completions" {
		t.Errorf("path %q — the configured escaping did not survive", seen.path)
	}
	// Both queries, the configured one first: one is the endpoint's routing
	// and one is this request's, and dropping either would be the desk
	// deciding something about an endpoint it does not read. The session
	// token, which the page necessarily sent, is in neither.
	if seen.rawQuery != "route=eu&stream=true" {
		t.Errorf("query %q, want the configured one and then the page's", seen.rawQuery)
	}
}

func TestRelayNeverForwardsTheSessionToken(t *testing.T) {
	// **The desk's own credential, in the place it is easiest to forget.** The
	// token travels as `?token=` on every route this chassis serves, so a relay
	// that forwarded the page's query verbatim would hand this desk's session
	// token to somebody else's endpoint on every relayed request. Caught by
	// this test being written before the rule was.
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	resp, body := relayGet(t, ts, "chat/completions?stream=true&token=another")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	if strings.Contains(seen.rawQuery, testToken) {
		t.Fatalf("the session token reached the endpoint: %q", seen.rawQuery)
	}
	// Every parameter of that name, not only the desk's own value: a rule
	// about what a parameter contains fails the first time one is spelled
	// differently.
	if strings.Contains(seen.rawQuery, "token") {
		t.Errorf("a token parameter reached the endpoint: %q", seen.rawQuery)
	}
	// And the page's own routing still travels.
	if seen.rawQuery != "stream=true" {
		t.Errorf("query %q, want the page's own parameters kept in order", seen.rawQuery)
	}
	if strings.Contains(fmt.Sprint(seen.header), testToken) {
		t.Errorf("the session token reached the endpoint in a header: %v", seen.header)
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
		{"a whole URL", "https://elsewhere.example/v1", "only letters, digits"},
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

func TestRelayBoundsABodyOfUndeclaredLength(t *testing.T) {
	// The second half of the bound. A chunked body declares no length, so the
	// check above cannot see it and the reader is what refuses it.
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
	_, ts, _ := assistantServer(t)
	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store")
	}
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
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":`+
		`{"url":"http://endpoint.example/v1","kind":"openai-compatible","model":"m","tools":[]}}}`)
	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatalf("store")
	}
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

/* What is written down ----------------------------------------------------- */

func TestRelayLogsNeitherTheKeyNorTheAddress(t *testing.T) {
	// A configured URL carrying both a path and a query, because those are the
	// two parts of an address a log must not carry: some gateways route on a
	// query, and a query is a place people put credentials.
	u := newUpstream(t, nil)
	_, ts, logged := relayDeskAt(t, "openai-compatible",
		u.server.URL+"/gateway?apikey=sk-in-the-query")
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	written := logged.String()
	// The event, and the origin, and nothing else. A log with nothing in it
	// would prove nothing about a handler that never ran, so the line is
	// required to be there.
	if !strings.Contains(written, "assistant relay") {
		t.Fatalf("nothing was logged about the relayed request: %q", written)
	}
	for _, forbidden := range []string{
		testKey, "sk-in-the-query", "/gateway", "chat/completions", "apikey",
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
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key sk-desk-test-0123456789-abcdefghij"}}`))
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
