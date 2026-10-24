package desk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAssistantDiscoveryFixedMetadata(t *testing.T) {
	for kind, path := range relayListingSuffix {
		t.Run(kind, func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":[],"models":[]}`) })
			_, ts, _ := relayDesk(t, kind, u)
			status, body := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/models", nil, map[string]string{"Anthropic-Beta": "caller-choice"})
			if status != http.StatusOK {
				t.Fatalf("%d %s", status, body)
			}
			got := u.only(t)
			if got.method != http.MethodGet || got.path != "/"+path || got.rawQuery != "" || len(got.body) != 0 {
				t.Fatalf("unexpected metadata request: %+v", got)
			}
			name, value, _ := credentialHeader(kind, "", testKey)
			if got.header.Get(name) != value {
				t.Fatal("stored credential was not presented")
			}
			if got.header.Get("Anthropic-Beta") != "" {
				t.Fatal("caller chose a provider header")
			}
		})
	}
}

func TestAssistantDiscoveryRefusesCallerPayload(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/api/assistant/models", map[string]string{"url": "https://other.invalid", "suffix": "chat/completions"}},
		{"/api/assistant/models?pageToken=next", nil},
		{"/api/assistant/models?", nil},
	} {
		code, _ := aiTestRequest(t, ts, http.MethodPost, tc.path, tc.body, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("%s: %d", tc.path, code)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/assistant/models", nil)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal(response.StatusCode)
	}
	if len(u.arrivals()) != 0 {
		t.Fatal("refused action reached endpoint")
	}
}

// A client can finish its headers and never send a body byte or EOF. This
// sentinel records that forbidden read without waiting for a network timeout.
type discoveryUnreadBody struct{ reads int }

func (b *discoveryUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.ErrUnexpectedEOF
}
func (*discoveryUnreadBody) Close() error { return nil }

func TestAssistantDiscoveryRefusesBodyWithoutReading(t *testing.T) {
	u := newUpstream(t, nil)
	s, _, _ := relayDesk(t, "openai-compatible", u)
	for _, tc := range []struct {
		name     string
		length   int64
		transfer []string
	}{
		{"declared nonempty", 1, nil},
		{"chunked", -1, []string{"chunked"}},
		{"unknown length", -1, nil},
		{"unframed body", 0, nil},
		{"invalid negative length", -2, nil},
		{"unknown transfer encoding", 0, []string{"unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &discoveryUnreadBody{}
			req := httptest.NewRequest(http.MethodPost, "/api/assistant/models", nil)
			bearer(req)
			req.Body, req.ContentLength, req.TransferEncoding = body, tc.length, tc.transfer
			reply := httptest.NewRecorder()
			s.handleAssistantModels(reply, req)
			if body.reads != 0 {
				t.Fatalf("body read %d times before refusal; a stalled input would block", body.reads)
			}
			if reply.Code != http.StatusBadRequest || reply.Header().Get(RefusalHeader) != string(CodeAssistantRelayPath) {
				t.Fatalf("expected coded body refusal: %d %s", reply.Code, reply.Body.String())
			}
			if len(u.arrivals()) != 0 {
				t.Fatal("refused body reached endpoint")
			}
		})
	}
}

func TestAssistantDiscoveryScansReply(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		status, want int
	}{
		{"empty listing", `{"data":[]}`, 200, 200},
		{"empty body", "", 200, 502},
		{"malformed", "{", 200, 502},
		{"oversized", `{"data":["` + strings.Repeat("a", maxListingBody) + `"]}`, 200, 502},
		{"key", `{"data":[{"id":"` + testKey + `"}]}`, 200, 502},
		{"refusal", `{"error":"private provider prose"}`, 403, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) })
			_, ts, _ := relayDesk(t, "openai-compatible", u)
			code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/models", nil, nil)
			if code != tc.want {
				t.Fatalf("%d %s", code, raw)
			}
			if tc.want == 502 && strings.Contains(string(raw), testKey) {
				t.Fatal("credential escaped scanner")
			}
		})
	}
}

func TestAssistantDiscoverySavedConnection(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"data":[]}`) })
	s, ts, _ := assistantServer(t)
	saved := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Fixture", u.server.URL)}})
	c := saved.Connections[0]
	headers := map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}
	if code, raw := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, headers); code != 200 {
		t.Fatalf("key: %d %s", code, raw)
	}
	for _, bad := range []map[string]string{nil, {aiConnectionHeader: testAIBeta, aiRevisionHeader: c.Revision}, {aiConnectionHeader: c.ID}, {aiConnectionHeader: c.ID, aiRevisionHeader: "stale"}} {
		if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/models", nil, bad); code != 409 {
			t.Fatalf("%d %s", code, raw)
		}
	}
	if len(u.arrivals()) != 0 {
		t.Fatal("wrong connection reached endpoint")
	}
	if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/models", nil, headers); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	// Moving the saved destination cannot move a credential bound to the old one.
	changed := saved.aiRegistry
	other := newUpstream(t, nil)
	changed.Connections[0].Assistant.Endpoint.URL = other.server.URL
	next := saveTestAIRegistry(t, s, ts, changed)
	headers[aiRevisionHeader] = next.Connections[0].Revision
	if code, raw := aiTestRequest(t, ts, http.MethodPost, "/api/assistant/models", nil, headers); code != 409 {
		t.Fatalf("binding: %d %s", code, raw)
	}
	if len(u.arrivals()) != 1 || len(other.arrivals()) != 0 {
		t.Fatal("unbound credential reached endpoint")
	}
}

func TestAssistantDiscoveryCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) })
	_, ts, _ := relayDesk(t, "openai-compatible", u)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/assistant/models", nil)
	bearer(req)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r, _ := ts.Client().Do(req)
		if r != nil {
			r.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("provider request did not cancel")
	}
	<-done
}

func TestAssistantDiscoveryBrowser(t *testing.T) {
	script := os.Getenv("JPS_ASSISTANT_DISCOVERY_BROWSER")
	if script == "" {
		t.Skip("set JPS_ASSISTANT_DISCOVERY_BROWSER after building web/dist")
	}
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"models":[{"name":"models/fixture-chat","displayName":"Fixture Chat","supportedGenerationMethods":["generateContent"]},{"name":"models/fixture-embedding","displayName":"Fixture Embedding","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"do-not-follow"}`)
	})
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, CodexBin: "off"})
	defer ts.Close()
	defer s.Close()
	c := aiTestConnection(testAIAlpha, "Fixture metadata", u.server.URL)
	c.Assistant.Endpoint.Kind = "gemini"
	saved := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: c.ID, Connections: []aiConnection{c}})
	c = saved.Connections[0]
	if code, body := aiTestRequest(t, ts, http.MethodPut, "/api/assistant/key", map[string]string{"key": testKey}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision}); code != 200 {
		t.Fatalf("key: %d %s", code, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", script, ts.URL, testToken)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("browser %v: %s", err, output)
	} else {
		t.Log(string(output))
	}
	arrivals := u.arrivals()
	if len(arrivals) != 2 {
		t.Fatalf("want reachability + first metadata page, got %d", len(arrivals))
	}
	for _, got := range arrivals {
		if got.method != "GET" || got.path != "/v1beta/models" || strings.Contains(got.rawQuery, "pageToken") {
			t.Fatalf("unexpected browser action: %+v", got)
		}
	}
}
