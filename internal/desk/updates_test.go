package desk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReleaseCheckPinsRepositoryAndRejectsUnpublishedVersions(t *testing.T) {
	for _, body := range []string{`{"tag_name":"v1.2.3"}`, `{"tag_name":"v1.2.3","prerelease":true}`, `{"tag_name":"v1.2.3","draft":true}`, `{"tag_name":"main"}`, `{"tag_name":"v1.2.3-rc.1"}`} {
		client := &http.Client{Transport: releaseTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != deskReleasesURL || r.Header.Get("Authorization") != "" {
				t.Fatal("repository or credentials changed")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
		got, err := fetchLatestDeskRelease(context.Background(), client)
		if body == `{"tag_name":"v1.2.3"}` {
			if err != nil || got["url"] != deskReleasePage+"/tag/v1.2.3" {
				t.Fatalf("%v %v", got, err)
			}
		} else if err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestUpdateAPIProtectsSourceCheckoutAndUsesExistingGuards(t *testing.T) {
	s, _ := newTestServer(t, true)
	for _, tc := range []struct {
		method, body, token, origin string
		want                        int
	}{
		{"GET", "", "", "", 401},
		{"POST", `{"action":"stage"}`, testToken, "https://evil.example", 403},
		{"GET", "", testToken, "", 200},
		{"POST", `{"action":"stage"}`, testToken, "", 409},
		{"POST", `{"action":"auto-on"}`, testToken, "", 409},
		{"POST", `{"action":"execute","url":"https://evil.example"}`, testToken, "", 400},
		{"POST", `{"action":"check"} {}`, testToken, "", 400},
	} {
		request := httptest.NewRequest(tc.method, "/api/updates", strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer "+tc.token)
		request.Header.Set("Origin", tc.origin)
		response := httptest.NewRecorder()
		s.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Errorf("%s %s: %d %s", tc.method, tc.body, response.Code, response.Body)
		}
		if tc.method == "GET" && tc.want == 200 && !strings.Contains(response.Body.String(), `"development":true`) {
			t.Fatal("development build not identified")
		}
	}
}
func TestDevelopmentLaunchNeverTrustsManagedEnvironment(t *testing.T) {
	t.Setenv("JPACK_DESK_INSTALL_ROOT", t.TempDir())
	if newUpdateService(true).root != "" {
		t.Fatal("development checkout acquired installation controls")
	}
}
