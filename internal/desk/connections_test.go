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
