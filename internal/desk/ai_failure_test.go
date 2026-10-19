package desk

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAIAgentFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{codexbridge.ErrBusy, "busy"}, {codexbridge.ErrSignIn, "authentication"}, {codexbridge.ErrRetries, "retries-exhausted"}, {codexbridge.ErrLimit, "provider-limit"}, {context.DeadlineExceeded, "timeout"}, {errors.New("PRIVATE_SENTINEL"), "unknown"}} {
		var got *aifailure.Failure
		if !errors.As(aiAgentFailure(tc.err), &got) || got.Code != tc.code {
			t.Fatalf("%v: %+v", tc.err, got)
		}
		data, _ := json.Marshal(got)
		if strings.Contains(string(data)+got.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("unsafe diagnostic")
		}
	}
	if aiAgentFailure(context.Canceled) != context.Canceled {
		t.Fatal("cancellation lost")
	}
	held := aifailure.New("storage", "storage", "not-started")
	if aiAgentFailure(held) != held {
		t.Fatal("typed failure lost")
	}
}
func TestAIResearchProviderFailureRetainsMetadataWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		w.Header().Set("x-request-id", "req_research")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"PRIVATE_SENTINEL"}}`))
	}))
	defer provider.Close()
	s, ts, _ := assistantServer(t)
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Research", provider.URL)}})
	connection := registry.Connections[0]
	code, _ := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "research-test-key"}, map[string]string{aiConnectionHeader: connection.ID, aiRevisionHeader: connection.Revision})
	if code != 200 {
		t.Fatal(code)
	}
	_, err := s.inputResearchModel(context.Background(), inputResearchAI{Connection: connection.ID, Revision: connection.Revision, Model: "shared-model"}, "Research fixtures", nil)
	var got *aifailure.Failure
	if !errors.As(err, &got) || got.Code != "configuration" || got.Status != 429 || got.RequestID != "req_research" {
		t.Fatalf("failure %+v, %v", got, err)
	}
	if deadline, e := time.Parse(time.RFC3339Nano, got.RetryAt); e != nil || time.Until(deadline) > 2*time.Second {
		t.Fatal("retry timing lost")
	}
	if strings.Contains(got.Error(), "PRIVATE_SENTINEL") || calls.Load() != 1 {
		t.Fatal("unsafe error or automatic replay")
	}
}
