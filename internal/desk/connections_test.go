package desk

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestConnectionControlsRequireDeskSessionAndRejectUnknownMethods(t *testing.T) {
	_, ts, _ := assistantServer(t)
	for _, authorized := range []bool{false, true} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/connections/unknown", strings.NewReader(`{}`))
		if authorized {
			authorizeAsPage(t, ts, req)
		}
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if authorized && res.StatusCode != 400 {
			t.Fatal(res.StatusCode)
		}
		if !authorized && res.StatusCode != 401 {
			t.Fatal(res.StatusCode)
		}
	}
}
func TestConnectionControlsDoNotFallbackFromExternalGateway(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	req, _ := http.NewRequest("POST", ts.URL+"/api/connections/status", strings.NewReader(`{}`))
	authorizeAsPage(t, ts, req)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(raw), `"unavailable"`) {
		t.Fatal(res.StatusCode, string(raw))
	}
	if s.connections.cmd != nil {
		t.Fatal("started local connections for external gateway")
	}
}

func TestUnavailableConnectionControlCannotReportConfigurationSuccess(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	req, _ := http.NewRequest("POST", ts.URL+"/api/connections/configure", strings.NewReader(`{"clientId":"test.apps.googleusercontent.com","clientSecret":"synthetic"}`))
	authorizeAsPage(t, ts, req)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal(res.StatusCode)
	}
	if s.connections.cmd != nil {
		t.Fatal("started local connections for external gateway")
	}
}

func TestCancelDoesNotStartALocalCompanionAfterGatewaySwitch(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	status, body := sendJSON(t, ts, "POST", "/api/connections/cancel", map[string]string{"id": strings.Repeat("a", 64)})
	if status != 200 || body["state"] != "canceled" {
		t.Fatal(status, body)
	}
	if s.connections.cmd != nil {
		t.Fatal("cleanup started a companion")
	}
}

func TestGmailRoutesStayAuthenticatedAndCannotFallback(t *testing.T) {
	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	for _, path := range []string{"/api/connections/gmail/search", "/api/connections/gmail/select", "/api/connections/gmail/configure"} {
		request, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(`{}`))
		response, err := ts.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatal(path, response.StatusCode)
		}
		status, _ := sendJSON(t, ts, "POST", path, map[string]string{})
		if status != 503 {
			t.Fatal(path, status)
		}
	}
	for _, path := range []string{"/api/connections/gmail/send", "/api/connections/gmail/pick", "/api/connections/google-drive/search", "/api/connections/unknown/status"} {
		status, _ := sendJSON(t, ts, "POST", path, map[string]string{})
		if status != 400 {
			t.Fatal(path, status)
		}
	}
	status, body := sendJSON(t, ts, "POST", "/api/connections/gmail/status", map[string]string{})
	if status != 200 || body["provider"] != "gmail" || body["state"] != "unavailable" {
		t.Fatal(status, body)
	}
	status, body = sendJSON(t, ts, "POST", "/api/connections/gmail/cancel", map[string]string{"id": strings.Repeat("a", 64)})
	if status != 200 || body["state"] != "canceled" || s.gmailConnections.cmd != nil || s.connections.cmd != nil {
		t.Fatal("unexpected companion", status, body)
	}
}
