package desk

import (
	"context"
	"encoding/json"
	"io"
	"log"
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

const testToken = "0123456789abcdef0123456789abcdef"

func newTestServer(t *testing.T, dev bool) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Config{
		ProjectDir: dir,
		JpackBin:   jpackBinary(),
		Token:      testToken,
		DevMode:    dev,
		Logger:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(func() {
		ts.Close()
		_ = s.Close()
	})
	return s, ts
}

// upgradeRequest issues a genuine WebSocket handshake so that a rejection is a
// rejection of an upgrade and not of a malformed request.
func upgradeRequest(t *testing.T, ts *httptest.Server, query, origin string) *http.Response {
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
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("upgrade request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestWSRequiresToken(t *testing.T) {
	_, ts := newTestServer(t, false)

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"no token at all", ""},
		{"empty token", "?token="},
		{"wrong token", "?token=deadbeefdeadbeefdeadbeefdeadbeef"},
		{"token of the right length but wrong bytes", "?token=" + strings.Repeat("f", len(testToken))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := upgradeRequest(t, ts, tc.query, ts.URL)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
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
			resp := upgradeRequest(t, ts, "?token="+testToken, origin)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
			}
		})
	}
}

// The token is checked before the origin, so a cross-origin page without the
// token learns nothing about which of the two it failed.
func TestWSTokenCheckedBeforeOrigin(t *testing.T) {
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

	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws?token="+testToken, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{ts.URL}},
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
	if resp := upgradeRequest(t, prod, "?token="+testToken, "http://localhost:5173"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("production status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	_, dev := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(dev)+"/ws?token="+testToken, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://localhost:5173"}},
	})
	if err != nil {
		t.Fatalf("dev mode should accept the Vite origin: %v", err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// A non-browser client sends no Origin; the token is its authorization.
func TestWSAllowsAbsentOrigin(t *testing.T) {
	_, ts := newTestServer(t, false)
	resp := upgradeRequest(t, ts, "?token="+testToken, "")
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("status = %d, want an upgrade rather than a refusal", resp.StatusCode)
	}
}

// TestRelayEndToEnd drives a real `jpack mcp` through the relay: initialize,
// then list_packs, against the real project tree. It is the test that would
// fail if the relay reframed, reordered, or dropped a JSON-RPC message.
func TestRelayEndToEnd(t *testing.T) {
	bin, project := e2eFixtures(t)

	s, err := New(Config{
		ProjectDir: project,
		JpackBin:   bin,
		Token:      testToken,
		Logger:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws?token="+testToken, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{ts.URL}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	c.SetReadLimit(readLimit)

	send := func(v any) {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// readResult waits for the response carrying id, skipping any
	// desk/fileChanged notification that arrives in the meantime.
	readResult := func(id float64) map[string]any {
		t.Helper()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if typ != websocket.MessageText {
				continue
			}
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("the relay delivered something that is not JSON: %v: %s", err, data)
			}
			if got, ok := msg["id"].(float64); !ok || got != id {
				continue
			}
			if errObj, ok := msg["error"]; ok {
				t.Fatalf("id %v returned an error: %v", id, errObj)
			}
			result, ok := msg["result"].(map[string]any)
			if !ok {
				t.Fatalf("id %v has no result object: %s", id, data)
			}
			return result
		}
	}

	send(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "jpack-desk-test", "version": "0"},
		},
	})
	initResult := readResult(1)
	server, ok := initResult["serverInfo"].(map[string]any)
	if !ok || server["name"] == "" {
		t.Fatalf("initialize returned no serverInfo: %v", initResult)
	}
	t.Logf("initialize: serverInfo=%v protocolVersion=%v", server, initResult["protocolVersion"])

	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	send(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "list_packs", "arguments": map[string]any{}},
	})
	callResult := readResult(2)
	if isErr, _ := callResult["isError"].(bool); isErr {
		t.Fatalf("list_packs reported a tool error: %v", callResult)
	}
	content, ok := callResult["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("list_packs returned no content: %v", callResult)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	var inventory struct {
		Status string `json:"status"`
		Packs  []struct {
			ID          string `json:"id"`
			PackID      string `json:"packId"`
			PackVersion string `json:"packVersion"`
		} `json:"packs"`
	}
	if err := json.Unmarshal([]byte(text), &inventory); err != nil {
		t.Fatalf("list_packs text is not the inventory JSON: %v", err)
	}
	if len(inventory.Packs) == 0 {
		t.Fatalf("list_packs returned an empty pack list for %s", project)
	}
	t.Logf("list_packs: status=%s packs=%d first=%s (%s %s)",
		inventory.Status, len(inventory.Packs),
		inventory.Packs[0].ID, inventory.Packs[0].PackID, inventory.Packs[0].PackVersion)

	// get_pack proves a large single-line payload survives the relay intact.
	send(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "get_pack", "arguments": map[string]any{"pack_id": inventory.Packs[0].ID}},
	})
	packResult := readResult(3)
	packText, _ := packResult["content"].([]any)[0].(map[string]any)["text"].(string)
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

// TestFileChangeNotification proves the one message the chassis originates
// reaches an open socket.
func TestFileChangeNotification(t *testing.T) {
	bin := requireBinary(t)
	project := t.TempDir()

	s, err := New(Config{
		ProjectDir: project,
		JpackBin:   bin,
		Token:      testToken,
		Logger:     log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(ts)+"/ws?token="+testToken, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{ts.URL}},
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
