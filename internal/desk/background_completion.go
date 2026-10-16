package desk

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
)

// The background transport uses the same normalized completion reasons as the
// frontend SDK adapters. Do not expose provider stop strings or raw responses in
// failures. Unlike chat synthesis, this boundary never retries a partial result.
func backgroundCompletion(kind string, raw []byte) (*aifailure.Completion, error) {
	var wire struct {
		Choices []struct {
			Reason  string `json:"finish_reason"`
			Message struct {
				ToolCalls    []json.RawMessage `json:"tool_calls"`
				FunctionCall json.RawMessage   `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
		Reason  string `json:"stop_reason"`
		Content []struct {
			Type string `json:"type"`
		} `json:"content"`
		Candidates []struct {
			Reason  string `json:"finishReason"`
			Content struct {
				Parts []struct {
					FunctionCall json.RawMessage `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Usage struct {
			Input      json.RawMessage `json:"input_tokens"`
			CacheRead  json.RawMessage `json:"cache_read_input_tokens"`
			CacheWrite json.RawMessage `json:"cache_creation_input_tokens"`
			Prompt     json.RawMessage `json:"prompt_tokens"`
			Output     json.RawMessage `json:"output_tokens"`
			Completion json.RawMessage `json:"completion_tokens"`
			Details    struct {
				Reasoning json.RawMessage `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
		GoogleUsage struct {
			Input     json.RawMessage `json:"promptTokenCount"`
			ToolInput json.RawMessage `json:"toolUsePromptTokenCount"`
			Output    json.RawMessage `json:"candidatesTokenCount"`
			Reasoning json.RawMessage `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return nil, aifailure.New("invalid-response", "model", "unknown")
	}
	reason := ""
	hasToolCalls := false
	result := &aifailure.Completion{FinishReason: "other"}
	switch kind {
	case "openai-compatible":
		if len(wire.Choices) == 1 {
			reason = wire.Choices[0].Reason
			hasToolCalls = len(wire.Choices[0].Message.ToolCalls) > 0 || nonNullCompletionValue(wire.Choices[0].Message.FunctionCall)
		}
		result.InputTokens = backgroundTokenCount(wire.Usage.Prompt)
		result.OutputTokens = backgroundTokenCount(wire.Usage.Completion)
		result.ReasoningTokens = backgroundTokenCount(wire.Usage.Details.Reasoning)
		switch reason {
		case "stop":
			result.FinishReason = "stop"
		case "length":
			result.FinishReason = "length"
		case "content_filter":
			result.FinishReason = "content-filter"
		case "tool_calls", "function_call":
			result.FinishReason = "tool-calls"
		}
	case "anthropic":
		reason = wire.Reason
		for _, part := range wire.Content {
			if part.Type == "tool_use" || part.Type == "server_tool_use" {
				hasToolCalls = true
			}
		}
		result.InputTokens = backgroundTokenTotal(wire.Usage.Input, wire.Usage.CacheRead, wire.Usage.CacheWrite)
		result.OutputTokens = backgroundTokenCount(wire.Usage.Output)
		switch reason {
		case "end_turn", "stop_sequence", "pause_turn":
			result.FinishReason = "stop"
		case "max_tokens", "model_context_window_exceeded":
			result.FinishReason = "length"
		case "refusal":
			result.FinishReason = "content-filter"
		case "tool_use":
			result.FinishReason = "tool-calls"
		}
	case "gemini":
		if len(wire.Candidates) == 1 {
			reason = wire.Candidates[0].Reason
			for _, part := range wire.Candidates[0].Content.Parts {
				if nonNullCompletionValue(part.FunctionCall) {
					hasToolCalls = true
				}
			}
		}
		result.InputTokens = backgroundTokenTotal(wire.GoogleUsage.Input, wire.GoogleUsage.ToolInput)
		result.OutputTokens = backgroundTokenTotal(wire.GoogleUsage.Output, wire.GoogleUsage.Reasoning)
		result.ReasoningTokens = backgroundTokenCount(wire.GoogleUsage.Reasoning)
		switch reason {
		case "STOP":
			result.FinishReason = "stop"
		case "MAX_TOKENS":
			result.FinishReason = "length"
		case "IMAGE_SAFETY", "RECITATION", "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			result.FinishReason = "content-filter"
		case "MALFORMED_FUNCTION_CALL":
			result.FinishReason = "error"
		}
		if wire.PromptFeedback.BlockReason != "" {
			reason = "blocked"
			result.FinishReason = "content-filter"
		}
	}
	if hasToolCalls && result.FinishReason != "length" && result.FinishReason != "content-filter" {
		reason = "tool-calls"
		result.FinishReason = "tool-calls"
	}
	// Older compatible endpoints omit completion metadata. Preserve their bounded
	// response for feature validation, while refusing every explicit incomplete stop.
	if reason == "" || result.FinishReason == "stop" {
		return result, nil
	}
	code := "invalid-response"
	if result.FinishReason == "length" {
		code = "output-limit"
	}
	if result.FinishReason == "content-filter" {
		code = "content-filter"
	}
	failure := aifailure.New(code, "model", "unknown")
	failure.Completion = result
	return result, failure
}

func backgroundTokenCount(raw json.RawMessage) *int64 {
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || value < 0 || value > 9007199254740991 {
		return nil
	}
	return &value
}

func nonNullCompletionValue(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func backgroundTokenTotal(parts ...json.RawMessage) *int64 {
	var sum int64
	present := false
	for _, part := range parts {
		if !nonNullCompletionValue(part) {
			continue
		}
		n := backgroundTokenCount(part)
		if n == nil || sum > 9007199254740991-*n {
			return nil
		}
		present = true
		sum += *n
	}
	if !present {
		return nil
	}
	return &sum
}
