package desk

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchRelayBudgetOnlyExtendsManagedSearch(t *testing.T) {
	for _, tt := range []struct {
		local       bool
		route, body string
		extended    bool
	}{
		{true, "acquire", `{"source":"web-search"}`, true},
		{false, "acquire", `{"source":"web-search"}`, false},
		{true, "seal", `{"source":"web-search"}`, false},
		{true, "acquire", `{"source":"gmail"}`, false},
		{true, "acquire", `broken`, false},
	} {
		total, idle := researchRequestTiming(tt.local, tt.route, []byte(tt.body))
		if tt.extended {
			if total != 140*time.Second || idle != 135*time.Second {
				t.Fatal("search envelope cuts off configured timeout")
			}
		} else if total != researchDeadline || idle != researchIdle {
			t.Fatal("unrelated request extended")
		}
	}
}

// shortResearchBounds shrinks every relay bound for one test.
func shortResearchBounds(t *testing.T, deadline, idle, searchDeadline, searchIdle time.Duration) {
	t.Helper()
	saved := [4]time.Duration{researchDeadline, researchIdle, researchSearchDeadline, researchSearchIdle}
	researchDeadline, researchIdle, researchSearchDeadline, researchSearchIdle = deadline, idle, searchDeadline, searchIdle
	t.Cleanup(func() {
		researchDeadline, researchIdle, researchSearchDeadline, researchSearchIdle = saved[0], saved[1], saved[2], saved[3]
	})
}

// managedResearchDesk is a desk with no desk-level file whose managed local
// gateway is the stand-in u.
func managedResearchDesk(t *testing.T, u *upstream) *httptest.Server {
	t.Helper()
	s, ts, _ := assistantServer(t)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	s.localGateway = &localGateway{bundle: t.TempDir(), done: done, pin: &localGatewayPin{
		URL: u.server.URL, Authority: "gateway:test", Signer: localSigner{Algorithm: "ed25519", Public: testSignerPublic}}}
	return ts
}

// Review round 1, finding 3. Through the handler: a managed local gateway's
// web-search acquire has its longer bounds, and any other acquire keeps the
// ordinary ones.
func TestTheRelayGivesOnlyManagedSearchItsLongerEnvelope(t *testing.T) {
	shortResearchBounds(t, 400*time.Millisecond, 200*time.Millisecond, 10*time.Second, 8*time.Second)
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(700 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	ts := managedResearchDesk(t, u)
	acquire := func(source string) (*http.Response, string) {
		return researchDo(t, ts, http.MethodPost, "acquire", strings.NewReader(`{"source":"`+source+`"}`),
			func(r *http.Request) { r.Header.Set("Content-Type", "application/json") })
	}
	if resp, body := acquire("web-search"); resp.StatusCode != http.StatusOK {
		t.Fatalf("managed search was cut off: %d %s", resp.StatusCode, body)
	}
	if resp, body := acquire("gmail"); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("another source was given search's envelope: %d %s", resp.StatusCode, body)
	}
}

// Review round 1, finding 3. The overall bound is counted from the request's
// arrival: a body that takes most of it leaves only the rest for the answer.
func TestTheRelaysDeadlineCountsTheBodysRead(t *testing.T) {
	shortResearchBounds(t, 900*time.Millisecond, 5*time.Second, 10*time.Second, 8*time.Second)
	u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(600 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	ts := managedResearchDesk(t, u)
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte(`{"source":`))
		time.Sleep(600 * time.Millisecond)
		_, _ = writer.Write([]byte(`"gmail"}`))
		_ = writer.Close()
	}()
	started := time.Now()
	resp, body := researchDo(t, ts, http.MethodPost, "acquire", reader, func(r *http.Request) {
		r.ContentLength = -1
		r.Header.Set("Content-Type", "application/json")
	})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("an answer past the bound counted from arrival was carried: %d %s after %v", resp.StatusCode, body, time.Since(started))
	}
}
