package desk

// The research relay's claims, measured at a recording gateway: no credential
// of any kind travels in either direction, exactly three routes are reachable
// and by exactly one method each, the body arrives verbatim and bounded, and
// the answer comes back with its type and nothing that would redirect the
// page or set a cookie against this desk.

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSignerPublic = "fc9fa9b25640778d85953a5e1b4618cb8223565b6f566f739c73035079d7681b"

// researchDeskFile is a desk-level file naming one research gateway.
func researchDeskFile(gatewayURL string) string {
	return fmt.Sprintf(`{"deskConfigVersion":1,"research":{"gateway":{"url":%q,"authority":"gateway:test",`+
		`"signer":{"algorithm":"ed25519","public":%q}},"sources":{"search":{"source":"search","dialect":"tavily-search"},`+
		`"read":{"source":"read","dialect":"jina-reader"}}}}`, gatewayURL, testSignerPublic)
}

func researchDesk(t *testing.T, u *upstream) (*Server, *httptest.Server) {
	t.Helper()
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, researchDeskFile(u.server.URL))
	return s, ts
}

func researchURL(ts *httptest.Server, suffix string) string {
	return ts.URL + researchPrefix + suffix
}

func researchDo(t *testing.T, ts *httptest.Server, method, suffix string, body io.Reader,
	decorate func(*http.Request)) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, researchURL(ts, suffix), body)
	if err != nil {
		t.Fatal(err)
	}
	if decorate != nil {
		decorate(req)
	}
	authorizeAsPage(t, ts, req)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, suffix, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

func TestResearchRelayCarriesAnAcquireVerbatimAndNoCredential(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "gw=never")
		w.Header().Set("Location", "/elsewhere")
		w.Header().Set(RefusalHeader, "forged")
		_, _ = w.Write([]byte(`{"result":{"ok":true},"receipt":{"sessionId":"s1"}}`))
	})
	_, ts := researchDesk(t, u)
	body := `{"session":"s1","source":"read","arguments":{"path":"/","body":{"url":"https://example.org"}}}`
	resp, answered := researchDo(t, ts, http.MethodPost, "acquire", strings.NewReader(body), func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "desk=secret")
		req.Header.Set("X-Api-Key", "never")
		req.Header.Set("Referer", "http://127.0.0.1:1/packs")
	})
	if resp.StatusCode != http.StatusOK || answered != `{"result":{"ok":true},"receipt":{"sessionId":"s1"}}` {
		t.Fatalf("answer: %d %s", resp.StatusCode, answered)
	}
	arrived := u.only(t)
	if arrived.method != http.MethodPost || arrived.path != "/acquire" || string(arrived.body) != body || arrived.rawQuery != "" {
		t.Fatalf("the request as the gateway saw it: %+v", arrived)
	}
	if arrived.contentLength != int64(len(body)) || len(arrived.transferEncoding) != 0 {
		t.Fatalf("a measured body is declared, never chunked: %+v", arrived)
	}
	for _, name := range []string{"Authorization", "Cookie", "X-Api-Key", "Origin", "Referer"} {
		if arrived.header.Get(name) != "" {
			t.Fatalf("%s reached the gateway: %v", name, arrived.header)
		}
	}
	if arrived.header.Get("Content-Type") != "application/json" {
		t.Fatalf("the content type travels: %v", arrived.header)
	}
	for _, name := range []string{"Set-Cookie", "Location", RefusalHeader} {
		if resp.Header.Get(name) != "" {
			t.Fatalf("%s came back to the page: %v", name, resp.Header)
		}
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("the answer's type comes back: %v", resp.Header)
	}
}

func TestResearchRelayReachesExactlyThreeRoutesByTheirMethods(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts := researchDesk(t, u)
	for _, refused := range []struct{ method, suffix string }{
		{http.MethodGet, "verify"}, {http.MethodPost, "act"}, {http.MethodGet, "publickey"},
		{http.MethodGet, "acquire"}, {http.MethodPost, "registry"}, {http.MethodDelete, "seal"},
		{http.MethodGet, ""}, {http.MethodGet, "registry/"}, {http.MethodGet, "Registry"},
		{http.MethodGet, "registry?session=s1"},
	} {
		resp, body := researchDo(t, ts, refused.method, refused.suffix, strings.NewReader("{}"), nil)
		if resp.StatusCode != http.StatusBadRequest || codeOfBody(t, body) != CodeResearchRelayPath {
			t.Errorf("%s %q: %d %s", refused.method, refused.suffix, resp.StatusCode, body)
		}
	}
	if len(u.arrivals()) != 0 {
		t.Fatalf("a refused route reached the gateway: %v", u.arrivals())
	}
	resp, _ := researchDo(t, ts, http.MethodGet, "registry", nil, nil)
	if resp.StatusCode != http.StatusOK || u.only(t).path != "/registry" || u.only(t).method != http.MethodGet {
		t.Fatalf("registry: %d %+v", resp.StatusCode, u.arrivals())
	}
}

func TestResearchRelayRefusesWithoutAGatewayAndSendsNothing(t *testing.T) {
	u := newUpstream(t, nil)
	s, ts, _ := assistantServer(t)
	counter := countingRelays(t)
	for name, file := range map[string]string{
		"no file":         "",
		"no research":     `{"deskConfigVersion":1}`,
		"null gateway":    `{"deskConfigVersion":1,"research":{"gateway":null}}`,
		"refused file":    `{"deskConfigVersion":1,"research":{"gateway":{"url":"` + u.server.URL + `","authority":"a","signer":{"algorithm":"ed25519","public":"` + testSignerPublic + `"}},"crawler":true}}`,
		"key in the file": `{"deskConfigVersion":1,"research":{"gateway":{"url":"` + u.server.URL + `","authority":"a","signer":{"algorithm":"ed25519","public":"` + testSignerPublic + `"},"apiKey":"x"}}}`,
	} {
		if file != "" {
			writeDeskConfig(t, s, file)
		}
		resp, body := researchDo(t, ts, http.MethodGet, "registry", nil, nil)
		if resp.StatusCode != http.StatusConflict || codeOfBody(t, body) != CodeResearchUnconfigured {
			t.Errorf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
	counter.mu.Lock()
	calls := counter.calls
	counter.mu.Unlock()
	if calls != 0 || len(u.arrivals()) != 0 {
		t.Fatal("a refused configuration made an outbound request")
	}
}

func TestResearchRelayBoundsTheBodyBeforeSending(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts := researchDesk(t, u)
	big := bytes.Repeat([]byte("x"), maxResearchBody+1)
	resp, body := researchDo(t, ts, http.MethodPost, "acquire", bytes.NewReader(big), nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || codeOfBody(t, body) != CodeTooLarge {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if len(u.arrivals()) != 0 {
		t.Fatal("a body past the bound reached the gateway")
	}
}

func TestResearchRelayReportsAGatewayThatNeverAnswered(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	resp, body := researchDo(t, ts, http.MethodGet, "registry", nil, nil)
	if resp.StatusCode != http.StatusBadGateway || codeOfBody(t, body) != CodeResearchRelayUpstream ||
		!strings.Contains(body, DiagnosticRefused) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestResearchRelayHoldsTheIdleBound(t *testing.T) {
	restore := researchIdle
	researchIdle = 150 * time.Millisecond
	t.Cleanup(func() { researchIdle = restore })
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	_, ts := researchDesk(t, u)
	started := time.Now()
	resp, body := researchDo(t, ts, http.MethodGet, "registry", nil, nil)
	if resp.StatusCode != http.StatusBadGateway || codeOfBody(t, body) != CodeResearchRelayUpstream {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("the idle bound did not apply: %v", time.Since(started))
	}
}

func TestResearchRelayRequiresTheDesksOwnSession(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts := researchDesk(t, u)
	req, _ := http.NewRequest(http.MethodGet, researchURL(ts, "registry"), nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || len(u.arrivals()) != 0 {
		t.Fatalf("an unauthenticated request reached the relay: %d", resp.StatusCode)
	}
}
