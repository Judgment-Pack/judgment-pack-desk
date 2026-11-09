package aifailure

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	agentcontract "github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	agentfailure "github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

func TestProviderFailuresAreStructuredAndBounded(t *testing.T) {
	for status, code := range map[int]string{401: "authentication", 403: "permission", 429: "rate-limited", 500: "unavailable", 529: "unavailable", 408: "timeout", 413: "context-limit", 400: "invalid-request"} {
		f := HTTP(status, http.Header{"X-Request-Id": []string{"req_fixture"}, "Retry-After": []string{"2"}, "Authorization": []string{"secret"}}, "model", time.Unix(0, 0))
		if f.Code != code || f.RequestID != "req_fixture" || f.RetryAt != "1970-01-01T00:00:02Z" {
			t.Fatalf("unexpected failure: %+v", f)
		}
		raw, _ := json.Marshal(f)
		if strings.Contains(string(raw)+f.Error(), "secret") {
			t.Fatal("raw provider data retained")
		}
	}
}
func TestRetryAfterAndUntrustedIdentifiers(t *testing.T) {
	f := HTTP(429, http.Header{"X-Request-Id": []string{"https://secret"}, "Retry-After": []string{"9999999999"}}, "model", time.Unix(0, 0))
	if f.RequestID != "" || f.RetryAt != "1970-01-02T00:00:00Z" {
		t.Fatalf("invalid metadata: %+v", f)
	}
	if New("unknown-code", "desk", "unknown").Code != "unknown" {
		t.Fatal("unsupported code accepted")
	}
}

func TestReleasedPaceFailureNormalization(t *testing.T) {
	for _, f := range []*Failure{New("pace exceeded", "desk", "not-started"), FromCode("pace exceeded", "desk", "not-started")} {
		if f.Code != "pace exceeded" || f.Recovery() != "wait" || f.Error() != agentfailure.Catalog["pace exceeded"].Message {
			t.Fatalf("released pace failure lost: %+v", f)
		}
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var wire agentcontract.Failure
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if parsed, ok := agentfailure.Read(wire); !ok || parsed.Code != "pace exceeded" || parsed.Outcome != "not-started" {
			t.Fatalf("published failure reader lost pace refusal: %+v", wire)
		}
	}
}
