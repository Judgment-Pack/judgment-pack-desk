package desk

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testToken is the **launch secret** every server in this package is built
// with. It is never on a request query: a script presents it as
// `Authorization: Bearer` (`bearer`), and a browser trades it once at
// `GET /launch` for the `jpack-desk-session` cookie (`launchSession`).
const testToken = "0123456789abcdef0123456789abcdef"

// testPort is the port a `Config` carries where the test never starts a server,
// or never exercises the cookie. It is a real requirement — the session
// cookie's name carries the port — and where the cookie *is* exercised the port
// comes from the listener instead; see `startDesk`.
const testPort = 8799

// startDesk builds one chassis and serves it, **listener first**.
//
// `Config.Port` is required, because the session cookie's name carries it, and
// `httptest.NewServer` does not allocate a port until it starts. An unstarted
// server allocates one immediately, so the chassis can be told the port it will
// actually be reached on — which is what `main` does, and what makes two test
// desks in one process name their cookies differently.
//
// It registers no cleanup: each caller already has the shape it wants, and one
// of them (the FIFO test) deliberately leaks a listener rather than hang.
func startDesk(t *testing.T, cfg Config) (*Server, *httptest.Server) {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	cfg.Port = ts.Listener.Addr().(*net.TCPAddr).Port
	s, err := New(cfg)
	if err != nil {
		ts.Close()
		t.Fatalf("New: %v", err)
	}
	ts.Config.Handler = s
	ts.Start()
	return s, ts
}

// browserHeader is what a browser puts on a same-origin WebSocket upgrade.
func browserHeader(cookie *http.Cookie, origin string) http.Header {
	header := http.Header{}
	header.Set("Cookie", cookie.Name+"="+cookie.Value)
	header.Set(fetchSiteHeader, fetchSiteSameOrigin)
	if origin != "" {
		header.Set("Origin", origin)
	}
	return header
}

// runtimeAvailable reports whether there is a `jpack` on this machine to drive.
//
// Not `requireBinary`, which skips the whole test: the **upgrade** half of the
// positive controls below needs no runtime and must run everywhere, and only
// the handshake half needs one.
func runtimeAvailable() bool {
	bin := jpackBinary()
	if !filepath.IsAbs(bin) {
		return false
	}
	_, err := os.Stat(bin)
	return err == nil
}

// acceptsSession is the positive control for a session, and it asserts **101
// and, where there is a runtime, a completed MCP handshake**.
//
// It used to be `upgradeRequest` plus "the status is not 401 and not 403",
// which a `500` satisfies: a chassis that authorized the request and then fell
// over passed every one of these. `websocket.Dial` returns an error unless the
// handshake completed with `101 Switching Protocols`, so the status is checked
// by the dial itself and again by name; and where a binary is available the
// socket is driven through `initialize`, which is the difference between "the
// upgrade was accepted" and "the relay behind it works".
func acceptsSession(t *testing.T, ts *httptest.Server, header http.Header) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("the upgrade did not complete: %v (status %d)", err, status)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	if !runtimeAvailable() {
		t.Log("no runtime binary: the upgrade was asserted, the MCP handshake was not")
		return
	}
	c.SetReadLimit(readLimit)
	(&rpcSession{t: t, ctx: ctx, ws: c}).initialize()
}

// sameOrigin marks a request the way a browser marks one it made to its own
// origin.
//
// **Every cookie-carrying request in this package goes through it**, because a
// cookie without it authorizes nothing — cookies have no port isolation, so the
// browser hands this desk's cookie to every page on every sibling port, and
// `Sec-Fetch-Site` is the only thing that tells those apart.
func sameOrigin(r *http.Request) {
	r.Header.Set(fetchSiteHeader, fetchSiteSameOrigin)
}

// bearer presents the launch secret the way every script, test and gate does.
func bearer(r *http.Request) {
	r.Header.Set("Authorization", "Bearer "+testToken)
}

// launchSession performs the launch exchange against a running test server and
// returns the session cookie it set.
//
// **The redirect is not followed.** `http.Client` follows a 303 by default, and
// following it here would hit the static handler and tell us nothing about the
// exchange; what this wants is the `Set-Cookie` on the exchange's own response.
func launchSession(t *testing.T, ts *httptest.Server) *http.Cookie {
	t.Helper()
	resp := launchResponse(t, ts, testToken)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("launch status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	// Whatever the exchange named it. The name carries the port, so a test that
	// spelled it out would be a test that knows which port `httptest` picked.
	if cookies := resp.Cookies(); len(cookies) == 1 {
		return cookies[0]
	}
	t.Fatalf("the launch exchange set %v, want exactly one session cookie", resp.Cookies())
	return nil
}

// launchResponse is one launch exchange, with redirects left unfollowed.
func launchResponse(t *testing.T, ts *httptest.Server, secret string) *http.Response {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(ts.URL + "/launch?secret=" + url.QueryEscape(secret))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	return resp
}

// withSession attaches a session cookie the way a browser does: the cookie, and
// the browser's own account of where the request came from.
func withSession(cookie *http.Cookie) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		sameOrigin(r)
	}
}

// withCookieOnly attaches the cookie and **nothing else** — the shape of a
// replay by a script, or of a request from a page on a sibling port.
func withCookieOnly(cookie *http.Cookie) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
}

func newTestServer(t *testing.T, dev bool) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	s, ts := startDesk(t, Config{
		ProjectDir: dir,
		JpackBin:   jpackBinary(),
		Token:      testToken,
		DevMode:    dev,
		Logger:     log.New(io.Discard, "", 0),
	})
	t.Cleanup(func() {
		ts.Close()
		_ = s.Close()
	})
	return s, ts
}

// upgradeRequest issues a genuine WebSocket handshake so that a rejection is a
// rejection of an upgrade and not of a malformed request.
//
// `decorate` is how the request is authorized — `bearer` for a script,
// `withSession(cookie)` for a browser, and nothing at all for the requests that
// must be refused. It is variadic rather than a mode, because "no credential"
// is then written as passing none rather than as a name for absence.
func upgradeRequest(t *testing.T, ts *httptest.Server, query, origin string, decorate ...func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/ws"+query, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, apply := range decorate {
		apply(req)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("upgrade request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestWSRequiresASession pins what does **not** open the relay, and the row
// that matters is the third: the launch secret on the query, which is exactly
// what authorized every request before this change and authorizes nothing now.
func TestWSRequiresASession(t *testing.T) {
	_, ts := newTestServer(t, false)

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"no credential at all", ""},
		{"an empty token parameter", "?token="},
		{"the launch secret on the query", "?token=" + testToken},
		{"the launch secret as ?secret=", "?secret=" + testToken},
		{"a wrong token parameter", "?token=deadbeefdeadbeefdeadbeefdeadbeef"},
		{"a token of the right length but wrong bytes", "?token=" + strings.Repeat("f", len(testToken))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := upgradeRequest(t, ts, tc.query, ts.URL)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}

// TestWSRefusesAWrongBearerSecret: the header is the script's door, and it is
// only a door with the right secret behind it.
func TestWSRefusesAWrongBearerSecret(t *testing.T) {
	_, ts := newTestServer(t, false)
	for _, header := range []string{
		"Bearer " + strings.Repeat("f", len(testToken)),
		"Bearer ",
		"Bearer",
		testToken,
		"Basic " + testToken,
	} {
		t.Run(header, func(t *testing.T) {
			resp := upgradeRequest(t, ts, "", ts.URL, func(r *http.Request) {
				r.Header.Set("Authorization", header)
			})
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}

// TestWSAcceptsTheSessionCookie is the browser's path: the launch exchange, and
// then an upgrade that carries nothing but the cookie.
func TestWSAcceptsTheSessionCookie(t *testing.T) {
	_, ts := newTestServer(t, false)
	acceptsSession(t, ts, browserHeader(launchSession(t, ts), ts.URL))
}

// TestWSRefusesAForgedSessionCookie: an id this desk never minted is not a
// session, however well formed it looks.
func TestWSRefusesAForgedSessionCookie(t *testing.T) {
	s, ts := newTestServer(t, false)
	real := launchSession(t, ts)
	for _, id := range []string{
		strings.Repeat("a", len(real.Value)),
		flipLast(real.Value),
		testToken,
		"",
	} {
		resp := upgradeRequest(t, ts, "", ts.URL, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: s.cookieName, Value: id})
			sameOrigin(r)
		})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("a forged id %q: status = %d, want %d", id, resp.StatusCode, http.StatusUnauthorized)
		}
	}
}

// TestWSRefusesACookieFromAForeignOrigin is the CSRF case the Origin guard now
// carries alone: the cookie is real, and the page sending it is not this desk's.
func TestWSRefusesACookieFromAForeignOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	cookie := launchSession(t, ts)
	resp := upgradeRequest(t, ts, "", "http://evil.example", withSession(cookie))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// TestWSAcceptsACookieWithNoOrigin: a same-site top-level navigation sends no
// Origin, and `SameSite=Strict` is why no foreign site can produce one.
func TestWSAcceptsACookieWithNoOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	acceptsSession(t, ts, browserHeader(launchSession(t, ts), ""))
}

func TestWSRejectsForeignOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)

	for _, origin := range []string{
		"http://evil.example",
		"https://evil.example",
		// A page on another loopback port is still another origin: the port is
		// part of the origin, and this is the case a token alone would miss.
		"http://127.0.0.1:9999",
		"null",
	} {
		t.Run(origin, func(t *testing.T) {
			resp := upgradeRequest(t, ts, "", origin, bearer)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
			}
		})
	}
}

// The session is checked before the origin, so a cross-origin page with no
// session learns nothing about which of the two it failed.
func TestWSSessionCheckedBeforeOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	resp := upgradeRequest(t, ts, "", "http://evil.example")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestWSAcceptsServedOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":        []string{ts.URL},
			"Authorization": []string{"Bearer " + testToken},
		},
	})
	if err != nil {
		t.Fatalf("dial with the served origin should succeed: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// The Vite dev server proxies /ws, so in dev mode the browser's Origin is the
// dev server's and never matches the Host. Only --dev-token opens that door.
func TestDevOriginOnlyInDevMode(t *testing.T) {
	_, prod := newTestServer(t, false)
	if resp := upgradeRequest(t, prod, "", "http://localhost:5173", bearer); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("production status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	_, dev := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(dev)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":        []string{"http://localhost:5173"},
			"Authorization": []string{"Bearer " + testToken},
		},
	})
	if err != nil {
		t.Fatalf("dev mode should accept the Vite origin: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// A non-browser client sends no Origin; the launch secret is its authorization.
func TestWSAllowsAbsentOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	resp := upgradeRequest(t, ts, "", "", bearer)
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("status = %d, want an upgrade rather than a refusal", resp.StatusCode)
	}
}

// TestRelayEndToEnd drives a real `jpack mcp` through the relay: initialize,
// then list_packs, against the real project tree. It is the test that would
// fail if the relay reframed, reordered, or dropped a JSON-RPC message.
func TestRelayEndToEnd(t *testing.T) {
	bin, project := e2eFixtures(t)

	s, ts := startDesk(t, Config{
		ProjectDir: project,
		JpackBin:   bin,
		Token:      testToken,
		Logger:     log.New(io.Discard, "", 0),
	})
	defer s.Close()
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":        []string{ts.URL},
			"Authorization": []string{"Bearer " + testToken},
		},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(readLimit)

	session := &rpcSession{t: t, ctx: ctx, ws: c}
	session.initialize()

	inventory := session.inventory(2)
	if len(inventory.Packs) == 0 {
		t.Fatalf("list_packs returned an empty pack list for %s", project)
	}
	t.Logf("list_packs: status=%s packs=%d first=%s (%s %s)",
		inventory.Status, len(inventory.Packs),
		inventory.Packs[0].ID, inventory.Packs[0].PackID, inventory.Packs[0].PackVersion)

	// get_pack proves a large single-line payload survives the relay intact.
	packResult := session.call(3, "get_pack", map[string]any{"pack_id": inventory.Packs[0].ID})
	packText := toolText(t, "get_pack", packResult)
	var doc map[string]any
	if err := json.Unmarshal([]byte(packText), &doc); err != nil {
		t.Fatalf("get_pack text is not a JSON document: %v", err)
	}
	for _, member := range []string{"specVersion", "id", "version", "title", "decision", "outcomes", "rules"} {
		if _, ok := doc[member]; !ok {
			t.Fatalf("the relayed pack document is missing the required member %q", member)
		}
	}
	t.Logf("get_pack: %d bytes, title=%q rules=%d", len(packText), doc["title"], len(doc["rules"].([]any)))
}

// TestRelayCarriesEvaluation drives the runtime's experimental evaluation
// surface through the relay: the call the desk's evaluation view makes, and the
// richest payload the desk reads back.
//
// The facts document is the empty object, which is the one document that says
// the same thing about every project: no fact pointer resolves, so every
// condition reading one is unknown. This test is about the wire and not about
// any pack's rules — it asserts that a disposition and a trace arrive whole,
// never which disposition a pack ought to reach.
//
// The project is copied first. A completed evaluation appends one record in a
// project whose configuration declares an audit directory, and a test must not
// write into the tree it was pointed at.
func TestRelayCarriesEvaluation(t *testing.T) {
	bin, project := e2eFixtures(t)
	copied := filepath.Join(t.TempDir(), "project")
	if err := copyTree(project, copied); err != nil {
		t.Fatalf("copying %s: %v", project, err)
	}

	s, ts := startDesk(t, Config{
		ProjectDir: copied,
		JpackBin:   bin,
		Token:      testToken,
		Logger:     log.New(io.Discard, "", 0),
	})
	defer s.Close()
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":        []string{ts.URL},
			"Authorization": []string{"Bearer " + testToken},
		},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(readLimit)

	session := &rpcSession{t: t, ctx: ctx, ws: c}
	session.initialize()

	inventory := session.inventory(2)
	if len(inventory.Packs) == 0 {
		t.Fatalf("list_packs returned an empty pack list for %s", copied)
	}

	result := session.call(3, "experimental_evaluate", map[string]any{
		"pack_id": inventory.Packs[0].ID,
		"facts":   "{}",
	})
	text := toolText(t, "experimental_evaluate", result)
	var payload struct {
		Experimental bool   `json:"experimental"`
		SpecVersion  string `json:"specVersion"`
		PackID       string `json:"packId"`
		Disposition  struct {
			Kind      string   `json:"kind"`
			OutcomeID string   `json:"outcomeId"`
			Reasons   []string `json:"reasons"`
			Handoff   struct {
				State       string   `json:"state"`
				TriggeredBy []string `json:"triggeredBy"`
			} `json:"handoff"`
		} `json:"disposition"`
		HandoffTarget *struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"handoffTarget"`
		Trace []struct {
			Stage     string `json:"stage"`
			ID        string `json:"id"`
			Condition string `json:"condition"`
		} `json:"trace"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("experimental_evaluate text is not the evaluation payload: %v", err)
	}
	if !payload.Experimental {
		t.Errorf("the payload must carry experimental = true: %s", text)
	}
	if payload.Disposition.Kind == "" {
		t.Fatalf("the payload carries no disposition kind: %s", text)
	}
	if payload.Disposition.Handoff.State == "" {
		t.Errorf("the disposition carries no handoff state: %s", text)
	}
	if len(payload.Trace) == 0 {
		t.Fatalf("the payload carries no trace entries: %s", text)
	}
	t.Logf("experimental_evaluate: pack=%s spec=%s kind=%s outcomeId=%q reasons=%v handoff=%s triggeredBy=%v target=%v",
		payload.PackID, payload.SpecVersion, payload.Disposition.Kind, payload.Disposition.OutcomeID,
		payload.Disposition.Reasons, payload.Disposition.Handoff.State,
		payload.Disposition.Handoff.TriggeredBy, payload.HandoffTarget)
	for _, entry := range payload.Trace {
		t.Logf("  trace: %-13s %-8s %s", entry.Stage, entry.Condition, entry.ID)
	}
}

// rpcSession is one open relay socket driven as a JSON-RPC client. The
// end-to-end tests share it so that each one is the calls it makes rather than
// the framing they all repeat.
type rpcSession struct {
	t   *testing.T
	ctx context.Context
	ws  *websocket.Conn
}

func (s *rpcSession) send(v any) {
	s.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		s.t.Fatalf("marshal: %v", err)
	}
	if err := s.ws.Write(s.ctx, websocket.MessageText, raw); err != nil {
		s.t.Fatalf("write: %v", err)
	}
}

// result waits for the response carrying id, skipping any desk/fileChanged
// notification that arrives in the meantime.
func (s *rpcSession) result(id float64) map[string]any {
	s.t.Helper()
	for {
		typ, data, err := s.ws.Read(s.ctx)
		if err != nil {
			s.t.Fatalf("read: %v", err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			s.t.Fatalf("the relay delivered something that is not JSON: %v: %s", err, data)
		}
		if got, ok := msg["id"].(float64); !ok || got != id {
			continue
		}
		if errObj, ok := msg["error"]; ok {
			s.t.Fatalf("id %v returned an error: %v", id, errObj)
		}
		result, ok := msg["result"].(map[string]any)
		if !ok {
			s.t.Fatalf("id %v has no result object: %s", id, data)
		}
		return result
	}
}

func (s *rpcSession) initialize() {
	s.t.Helper()
	s.send(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "jpack-desk-test", "version": "0"},
		},
	})
	initResult := s.result(1)
	server, ok := initResult["serverInfo"].(map[string]any)
	if !ok || server["name"] == "" {
		s.t.Fatalf("initialize returned no serverInfo: %v", initResult)
	}
	s.t.Logf("initialize: serverInfo=%v protocolVersion=%v", server, initResult["protocolVersion"])
	s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func (s *rpcSession) call(id float64, name string, args map[string]any) map[string]any {
	s.t.Helper()
	s.send(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	result := s.result(id)
	if isErr, _ := result["isError"].(bool); isErr {
		s.t.Fatalf("%s reported a tool error: %v", name, result)
	}
	return result
}

// packInventory is the part of the runtime's list_packs answer these tests read.
type packInventory struct {
	Status string `json:"status"`
	Packs  []struct {
		ID          string `json:"id"`
		PackID      string `json:"packId"`
		PackVersion string `json:"packVersion"`
	} `json:"packs"`
}

func (s *rpcSession) inventory(id float64) packInventory {
	s.t.Helper()
	text := toolText(s.t, "list_packs", s.call(id, "list_packs", map[string]any{}))
	var inventory packInventory
	if err := json.Unmarshal([]byte(text), &inventory); err != nil {
		s.t.Fatalf("list_packs text is not the inventory JSON: %v", err)
	}
	return inventory
}

// toolText is the first text content block of a tool result: where every jpack
// tool puts the JSON it answers with.
func toolText(t *testing.T, name string, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("%s returned no content: %v", name, result)
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("%s returned a content block that is not an object: %v", name, content[0])
	}
	text, _ := block["text"].(string)
	if text == "" {
		t.Fatalf("%s returned no text content: %v", name, block)
	}
	return text
}

// copyTree copies a directory tree. It carries directories and regular files
// and nothing else: a project is documents, and a symlink or a device node in
// one is not something a test should reproduce.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// TestFileChangeNotification proves the one message the chassis originates
// reaches an open socket.
func TestFileChangeNotification(t *testing.T) {
	bin := requireBinary(t)
	project := t.TempDir()

	s, ts := startDesk(t, Config{
		ProjectDir: project,
		JpackBin:   bin,
		Token:      testToken,
		Logger:     log.New(io.Discard, "", 0),
	})
	defer s.Close()
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":        []string{ts.URL},
			"Authorization": []string{"Bearer " + testToken},
		},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if err := os.WriteFile(filepath.Join(project, "some.pack.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("never received desk/fileChanged: %v", err)
		}
		var msg struct {
			Method string `json:"method"`
			Params struct {
				Path string `json:"path"`
			} `json:"params"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Method == "desk/fileChanged" {
			if msg.Params.Path != "some.pack.json" {
				t.Fatalf("path = %q, want %q", msg.Params.Path, "some.pack.json")
			}
			t.Logf("desk/fileChanged: path=%s", msg.Params.Path)
			return
		}
	}
}

func TestSPAFallbackWithoutBuiltAssets(t *testing.T) {
	// With no Static configured the chassis must say so rather than panic.
	_, ts := newTestServer(t, false)
	resp, err := ts.Client().Get(ts.URL + "/packs/anything")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func wsURL(ts *httptest.Server) string {
	u, _ := url.Parse(ts.URL)
	u.Scheme = "ws"
	return u.String()
}

// jpackBinary is the runtime under test: JPACK_BIN, else ./bin/jpack relative
// to the repository root, else whatever PATH resolves.
func jpackBinary() string {
	if v := os.Getenv("JPACK_BIN"); v != "" {
		return v
	}
	if abs, err := filepath.Abs(filepath.Join("..", "..", "bin", "jpack")); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return "jpack"
}

// requireBinary returns the runtime binary, skipping when there is none to
// drive.
func requireBinary(t *testing.T) string {
	t.Helper()
	bin := jpackBinary()
	if !filepath.IsAbs(bin) {
		t.Skip("no runtime binary: build one to ./bin/jpack or set JPACK_BIN")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no runtime binary at %s: %v", bin, err)
	}
	return bin
}

// e2eFixtures returns the runtime binary and the project tree for the
// end-to-end tests, skipping when either has not been provided.
func e2eFixtures(t *testing.T) (bin, project string) {
	t.Helper()
	bin = requireBinary(t)
	project = os.Getenv("JPACK_PROJECT")
	if project == "" {
		t.Skip("set JPACK_PROJECT to a Judgment Pack project directory to run the end-to-end tests")
	}
	if _, err := os.Stat(filepath.Join(project, "jpack.json")); err != nil {
		t.Skipf("JPACK_PROJECT=%s has no jpack.json: %v", project, err)
	}
	return bin, project
}
