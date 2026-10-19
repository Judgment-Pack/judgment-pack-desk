package desk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// relayDeskConfigured is a relay to an upstream, configured with the members
// the caller names and, unless noKey, with a key stored for it.
func relayDeskConfigured(t *testing.T, u *upstream, members string, noKey bool) (*Server, *httptest.Server) {
	t.Helper()
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, fmt.Sprintf(`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":%q,"kind":"openai-compatible","model":"a-model","tools":[]%s}}}`, u.server.URL, members))
	if !noKey {
		if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
			t.Fatalf("store: %d %v", status, body)
		}
	}
	return s, ts
}

// An endpoint whose owner says it takes the key in the api-key header gets it
// there, in that form, and not as a bearer.
func TestRelayPresentsTheKeyInTheAPIKeyHeaderWhereTheEndpointTakesIt(t *testing.T) {
	for _, tc := range []struct {
		name, members, header, value string
		absent                       string
	}{
		{"the protocol's own form", ``, "Authorization", "Bearer " + testKey, "api-key"},
		{"bearer said outright", `,"auth":"bearer"`, "Authorization", "Bearer " + testKey, "api-key"},
		{"the api-key form", `,"auth":"api-key"`, "api-key", testKey, "Authorization"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newUpstream(t, nil)
			_, ts := relayDeskConfigured(t, u, tc.members, false)
			resp, body := relayGet(t, ts, "chat/completions")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			seen := u.only(t)
			if got := seen.header.Get(tc.header); got != tc.value {
				t.Errorf("%s: %q, want %q", tc.header, got, tc.value)
			}
			if got := seen.header.Get(tc.absent); got != "" {
				t.Errorf("%s was sent as well: %q", tc.absent, got)
			}
		})
	}
}

func TestAPIKeyHeaderIsOnlyAFormOfAnOpenAICompatibleEndpoint(t *testing.T) {
	for _, kind := range []string{"anthropic", "gemini"} {
		config := fmt.Sprintf(`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"https://e.example","kind":%q,"model":"m","tools":[],"auth":"api-key"}}}`, kind)
		if decoded := decodeDeskFile([]byte(config)); !decoded.refused() {
			t.Errorf("%s: accepted an api-key header form it does not have", kind)
		}
	}
	for _, kind := range []string{"anthropic", "gemini"} {
		name, _, ok := credentialHeader(kind, "api-key", testKey)
		if !ok || name == "api-key" {
			t.Errorf("%s: the api-key form leaked into its own: %q", kind, name)
		}
	}
}

// An endpoint on this computer needs no key: with none stored the relay sends
// the request bare, and with one stored it presents it as ever.
func TestRelaySendsNoCredentialToAnEndpointOnThisComputerWithNoKey(t *testing.T) {
	u := newUpstream(t, nil) // httptest listens on 127.0.0.1
	_, ts := relayDeskConfigured(t, u, ``, true)
	resp, body := relayGet(t, ts, "chat/completions")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	seen := u.only(t)
	for _, name := range []string{"Authorization", "api-key", "x-api-key", "x-goog-api-key"} {
		if got := seen.header.Get(name); got != "" {
			t.Errorf("%s was sent to a keyless endpoint: %q", name, got)
		}
	}
}

func TestRelayStillPresentsAStoredKeyToAnEndpointOnThisComputer(t *testing.T) {
	u := newUpstream(t, nil)
	_, ts := relayDeskConfigured(t, u, ``, false)
	if resp, body := relayGet(t, ts, "chat/completions"); resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if got := u.only(t).header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization %q, want the stored key", got)
	}
}

// Nothing else is keyless: a plain http: address on this network (whatever the
// owner has opted into) and an https: address are refused with no key, and
// nothing is sent.
func TestRelayRefusesEveryOtherEndpointWithNoKey(t *testing.T) {
	counter := countingRelays(t)
	for _, config := range []string{
		`{"url":"https://e.example/v1","kind":"openai-compatible","model":"m","tools":[]}`,
		`{"url":"http://192.168.1.20:8000/v1","kind":"openai-compatible","model":"m","tools":[],"lan":true}`,
		`{"url":"http://gpu-box:8000/v1","kind":"openai-compatible","model":"m","tools":[],"lan":true}`,
	} {
		s, ts, _ := assistantServer(t)
		writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":`+config+`}}`)
		resp, body := relayGet(t, ts, "chat/completions")
		if resp.StatusCode != http.StatusConflict || codeOfBody(t, body) != CodeAssistantNoKey {
			t.Errorf("%s: status %d code %q, want 409 %s", config, resp.StatusCode, codeOfBody(t, body), CodeAssistantNoKey)
		}
	}
	if calls, _ := counter.seen(); calls != 0 {
		t.Fatalf("a keyless request to an endpoint that needs a key made %d outbound request(s)", calls)
	}
}

func TestTheKeyAnswerSaysKeylessOnlyForAnEndpointOnThisComputer(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		keyless   bool
	}{
		{"localhost", "http://localhost:11434/v1", true},
		{"loopback address", "http://127.0.0.1:8000/v1", true},
		{"a public https address", "https://api.example.invalid/v1", false},
		{"a private network address", "http://192.168.1.20:8000/v1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			lan := ""
			if tc.name == "a private network address" {
				lan = `,"lan":true`
			}
			writeDeskConfig(t, s, fmt.Sprintf(`{"deskConfigVersion":1,"assistant":{"endpoint":{"url":%q,"kind":"openai-compatible","model":"m","tools":[]%s}}}`, tc.url, lan))
			status, body := sendJSON(t, ts, http.MethodGet, "/api/assistant/key", nil)
			if status != http.StatusOK {
				t.Fatalf("status %d: %v", status, body)
			}
			if got, _ := body["keyless"].(bool); got != tc.keyless {
				raw, _ := json.Marshal(body)
				t.Errorf("keyless %v, want %v: %s", got, tc.keyless, raw)
			}
			if present, _ := body["present"].(bool); present {
				t.Errorf("a key is reported present on a desk that stored none")
			}
		})
	}
}

// A plain http: address on this network is accepted only where the owner said
// so, and only for an address on one.
func TestTransportRuleAdmitsAPrivateNetworkAddressOnlyWithTheOptIn(t *testing.T) {
	for _, tc := range []struct {
		url     string
		lan, ok bool
	}{
		{"http://10.0.0.5/v1", true, true},
		{"http://172.16.0.1/v1", true, true},
		{"http://172.31.255.254/v1", true, true},
		{"http://192.168.0.2:9000/v1", true, true},
		{"http://gpu-box:8000/v1", true, true},
		{"http://llm.local/v1", true, true},
		{"http://llm.lan/v1", true, true},
		{"http://printer.home.arpa/v1", true, true},
		{"http://10.0.0.5/v1", false, false},
		{"http://172.32.0.1/v1", true, false},
		{"http://172.15.0.1/v1", true, false},
		{"http://11.0.0.5/v1", true, false},
		{"http://192.169.0.1/v1", true, false},
		{"http://203.0.113.9/v1", true, false},
		{"http://llm.example.com/v1", true, false},
		{"http://[fd00::1]/v1", true, false},
		{"http://256.0.0.1/v1", true, false},
		{"https://llm.example.com/v1", false, true},
		{"http://127.0.0.1:11434/v1", false, true},
	} {
		got := endpointURLProblem(tc.url, tc.lan) == ""
		if got != tc.ok {
			t.Errorf("%s (lan %t): accepted %t, want %t", tc.url, tc.lan, got, tc.ok)
		}
	}
}
