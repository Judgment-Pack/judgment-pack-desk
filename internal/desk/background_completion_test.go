package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
)

func TestBackgroundCompletionRefusesPartialJSONWithoutRetry(t *testing.T) {
	for _, tc := range []struct{ kind, reason, code, normalized string }{
		{"openai-compatible", "length", "output-limit", "length"},
		{"anthropic", "max_tokens", "output-limit", "length"},
		{"gemini", "MAX_TOKENS", "output-limit", "length"},
		{"openai-compatible", "content_filter", "content-filter", "content-filter"},
		{"anthropic", "refusal", "content-filter", "content-filter"},
		{"gemini", "SAFETY", "content-filter", "content-filter"},
		{"gemini", "MALFORMED_FUNCTION_CALL", "invalid-response", "error"},
		{"openai-compatible", "tool_calls", "invalid-response", "tool-calls"},
		{"anthropic", "PRIVATE_PROVIDER_REASON", "invalid-response", "other"},
	} {
		t.Run(tc.kind+"-"+tc.reason, func(t *testing.T) {
			var calls atomic.Int32
			s, _, _, task := backgroundAPIFixture(t, tc.kind, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				// Syntactically valid JSON can still be an incomplete model response.
				text := `{"fields":{"one":false}}`
				var response any
				switch tc.kind {
				case "openai-compatible":
					response = map[string]any{"choices": []any{map[string]any{"finish_reason": tc.reason, "message": map[string]any{"content": text}}}, "usage": map[string]any{"prompt_tokens": 17, "completion_tokens": 8, "completion_tokens_details": map[string]any{"reasoning_tokens": 3}}}
				case "anthropic":
					response = map[string]any{"stop_reason": tc.reason, "content": []any{map[string]any{"type": "text", "text": text}}, "usage": map[string]any{"input_tokens": 17, "output_tokens": 8}}
				case "gemini":
					response = map[string]any{"candidates": []any{map[string]any{"finishReason": tc.reason, "content": map[string]any{"parts": []any{map[string]any{"text": text}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 17, "candidatesTokenCount": 8, "thoughtsTokenCount": 3}}
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			result, err := s.runBackgroundModel(backgroundFixtureContext(t, &task), task, nil)
			var failure *aifailure.Failure
			if len(result) != 0 || !errors.As(err, &failure) || failure.Code != tc.code || failure.Completion == nil || failure.Completion.FinishReason != tc.normalized || calls.Load() != 1 {
				t.Fatalf("result %s failure %+v calls %d", result, failure, calls.Load())
			}
			completion := failure.Completion
			expectedOutput := int64(8)
			if tc.kind == "gemini" {
				expectedOutput = 11
			}
			if completion.InputTokens == nil || *completion.InputTokens != 17 || completion.OutputTokens == nil || *completion.OutputTokens != expectedOutput {
				t.Fatalf("usage lost: %+v", completion)
			}
			if tc.kind != "anthropic" && (completion.ReasoningTokens == nil || *completion.ReasoningTokens != 3) {
				t.Fatal("reasoning count lost")
			}
			raw, _ := json.Marshal(failure)
			if strings.Contains(string(raw), "PRIVATE_PROVIDER_REASON") || strings.Contains(string(raw), `fields`) {
				t.Fatal("provider output escaped into failure")
			}
		})
	}
}

func TestBackgroundCompletionHandlesMissingAndUnsafeMetadata(t *testing.T) {
	for _, tc := range []struct{ kind, raw string }{
		{"openai-compatible", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":-1,"completion_tokens":9007199254740992,"completion_tokens_details":{"reasoning_tokens":"3"}}}`},
		{"anthropic", `{"stop_reason":"end_turn","usage":{"input_tokens":1.5,"output_tokens":null}}`},
		{"gemini", `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":{},"candidatesTokenCount":true,"thoughtsTokenCount":1e50}}`},
		{"openai-compatible", `{"choices":[{}]}`},
	} {
		got, err := backgroundCompletion(tc.kind, []byte(tc.raw))
		if err != nil || got.InputTokens != nil || got.OutputTokens != nil || got.ReasoningTokens != nil {
			t.Fatalf("unsafe metadata %s %+v %v", tc.kind, got, err)
		}
	}
	_, err := backgroundCompletion("gemini", []byte(`{"promptFeedback":{"blockReason":"PRIVATE_REASON"}}`))
	var failure *aifailure.Failure
	if !errors.As(err, &failure) || failure.Code != "content-filter" {
		t.Fatalf("prompt refusal %v", err)
	}
}

func TestBackgroundCompletionRejectsUnrequestedToolsBesideCompleteText(t *testing.T) {
	for _, tc := range []struct{ kind, raw string }{
		{"gemini", `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{\"fields\":{}}"},{"functionCall":{"name":"unknown","args":{}}}]}}]}`},
		{"openai-compatible", `{"choices":[{"finish_reason":"stop","message":{"content":"{\"fields\":{}}","tool_calls":[{"function":{"name":"unknown"}}]}}]}`},
		{"openai-compatible", `{"choices":[{"message":{"content":"{\"fields\":{}}","function_call":{"name":"unknown"}}}]}`},
		{"anthropic", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"fields\":{}}"},{"type":"tool_use","name":"unknown"}]}`},
	} {
		got, err := backgroundCompletion(tc.kind, []byte(tc.raw))
		if err == nil || got.FinishReason != "tool-calls" {
			t.Fatalf("tools accepted: %s %+v %v", tc.kind, got, err)
		}
	}
}

func TestBackgroundCompletionUsageTotalsAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		kind, raw     string
		input, output int64
	}{
		{"anthropic", `{"stop_reason":"end_turn","usage":{"input_tokens":17,"cache_read_input_tokens":5,"cache_creation_input_tokens":7,"output_tokens":8}}`, 29, 8},
		{"gemini", `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":17,"toolUsePromptTokenCount":5,"candidatesTokenCount":8,"thoughtsTokenCount":3}}`, 22, 11},
		{"gemini", `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"thoughtsTokenCount":3}}`, 0, 3},
	} {
		got, err := backgroundCompletion(tc.kind, []byte(tc.raw))
		if err != nil || got.InputTokens == nil || *got.InputTokens != tc.input || got.OutputTokens == nil || *got.OutputTokens != tc.output {
			t.Fatalf("usage totals: %+v %v", got, err)
		}
	}
	got, err := backgroundCompletion("gemini", []byte(`{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":9007199254740991,"thoughtsTokenCount":1}}`))
	if err != nil || got.OutputTokens != nil || got.ReasoningTokens == nil || *got.ReasoningTokens != 1 {
		t.Fatalf("overflow: %+v %v", got, err)
	}
}

// The cache figures, as the page's harness records them since v0.1.0: read
// from each wire where the provider reports them, absent where it does not, and
// kept when a completion is written and read back as JSON.
func TestBackgroundCompletionAdmitsCacheFigures(t *testing.T) {
	count := func(n int64) *int64 { return &n }
	for _, tc := range []struct {
		kind, raw   string
		read, write *int64
	}{
		{"openai-compatible", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":17,"prompt_tokens_details":{"cached_tokens":12},"completion_tokens":8}}`, count(12), nil},
		{"anthropic", `{"stop_reason":"end_turn","usage":{"input_tokens":17,"cache_read_input_tokens":5,"cache_creation_input_tokens":7,"output_tokens":8}}`, count(5), count(7)},
		{"gemini", `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":17,"cachedContentTokenCount":9,"candidatesTokenCount":8}}`, count(9), nil},
		{"openai-compatible", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":17,"completion_tokens":8}}`, nil, nil},
		{"anthropic", `{"stop_reason":"end_turn","usage":{"input_tokens":17,"cache_read_input_tokens":-1,"cache_creation_input_tokens":"7","output_tokens":8}}`, nil, nil},
	} {
		got, err := backgroundCompletion(tc.kind, []byte(tc.raw))
		if err != nil {
			t.Fatal(tc.kind, err)
		}
		same := func(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
		if !same(got.CacheReadTokens, tc.read) || !same(got.CacheWriteTokens, tc.write) {
			t.Fatalf("%s %s: cache figures %v %v", tc.kind, tc.raw, got.CacheReadTokens, got.CacheWriteTokens)
		}
	}
	raw := []byte(`{"version":1,"code":"output-limit","source":"model","outcome":"unknown","completion":{"finishReason":"length","inputTokens":29,"outputTokens":8,"cacheReadTokens":5,"cacheWriteTokens":7}}`)
	var failure aifailure.Failure
	if err := json.Unmarshal(raw, &failure); err != nil {
		t.Fatal(err)
	}
	if c := failure.Completion; c == nil || c.CacheReadTokens == nil || *c.CacheReadTokens != 5 || c.CacheWriteTokens == nil || *c.CacheWriteTokens != 7 {
		t.Fatalf("cache figures dropped on read: %+v", failure.Completion)
	}
	again, _ := json.Marshal(failure)
	if !strings.Contains(string(again), `"cacheReadTokens":5`) || !strings.Contains(string(again), `"cacheWriteTokens":7`) {
		t.Fatalf("cache figures dropped on write: %s", again)
	}
}
