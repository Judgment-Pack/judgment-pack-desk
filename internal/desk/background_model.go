package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// backgroundModelChoice pins the installation connection and exact model. It is
// shared by feature adapters; the original research journal encoding is unchanged.
type backgroundModelChoice struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Model      string `json:"model"`
	Effort     string `json:"effort,omitempty"`
}

const backgroundModelResponseBytes = 128 << 10

// A host-authored contract, never supplied directly by browser or model callers.
// Validate gates subscription tool submissions; API results remain exact completed
// responses (including malformed JSON as a string) for journaled host correction.
type backgroundModelRequest struct {
	Choice       backgroundModelChoice
	Owner        string
	Prompt       string
	Instructions string
	Response     codexbridge.Tool
	Validate     func(json.RawMessage) error
}

func backgroundAIRequest(ctx context.Context, choice backgroundModelChoice) *http.Request {
	r, _ := http.NewRequestWithContext(ctx, "POST", "http://desk.invalid/", nil)
	r.Header.Set(aiConnectionHeader, choice.Connection)
	r.Header.Set(aiRevisionHeader, choice.Revision)
	return r
}
func (s *Server) validateBackgroundModelConnection(ctx context.Context, choice backgroundModelChoice) error {
	c, e := s.selectedAIConnection(backgroundAIRequest(ctx, choice), true)
	if e != nil {
		return e
	}
	if c == nil {
		return aifailure.Authored("configuration", "model", "not-started", "Select a named AI connection.")
	}
	p, e := s.readAssistantProfile()
	if e != nil {
		return aifailure.Authored("configuration", "model", "not-started", assistantProfileProblem)
	}
	if !contains(connectionModels(c, p), choice.Model) {
		return aifailure.Authored("permission", "model", "not-started", "The model is not enabled for this connection and desk.")
	}
	if c.Assistant.Engine == "codex" {
		return nil
	}
	endpoint, keyName, _, e := s.endpointForConnection(backgroundAIRequest(ctx, choice), true)
	if e != nil {
		return e
	}
	stored, e := s.assistant.readKeyNamed(keyName)
	if e != nil || !stored.present {
		return aifailure.Authored("authentication", "model", "not-started", "This AI connection needs its saved API key.")
	}
	if reason := bindingProblem(stored, endpoint); reason != "" {
		return aifailure.Authored("configuration", "model", "not-started", reason)
	}
	// API reasoning defaults are kept until a shared server-side mapping exists;
	// never silently reinterpret a native effort as a different protocol's budget.
	if choice.Effort != "" && endpoint.kind != "openai-compatible" {
		return aifailure.Authored("configuration", "model", "not-started", "Use the provider default reasoning setting for this API connection.")
	}
	return nil
}

// runBackgroundModel performs one admitted model operation. The owning workflow
// must journal admission, Plan/approval and allowance checks before calling it.
// This transport neither grants permission nor retries or executes proposed actions.
func (s *Server) runBackgroundModel(ctx context.Context, task backgroundModelRequest, availability func(bool) error) (result json.RawMessage, modelErr error) {
	choice, prompt := task.Choice, task.Prompt
	defer func() { modelErr = aiModelFailure(modelErr) }()
	if task.Owner == "" || task.Instructions == "" || task.Response.Name == "" || task.Response.Description == "" || !json.Valid(task.Response.InputSchema) || task.Validate == nil {
		return nil, aifailure.Authored("configuration", "model", "not-started", "The background model response contract is incomplete.")
	}
	if e := s.validateBackgroundModelConnection(ctx, choice); e != nil {
		return nil, e
	}
	policy, _, err := s.aiContext(choice.Model)
	if err != nil {
		return nil, err
	}
	request := backgroundAIRequest(ctx, choice)
	connection, e := s.selectedAIConnection(request, true)
	if e != nil {
		return nil, e
	}
	if connection.Assistant.Engine == "codex" {
		account, e := s.accountForConnection(request)
		if e != nil {
			return nil, e
		}
		runner, ok := account.(providerRunner)
		if !ok {
			return nil, codexbridge.ErrUnavailable
		}
		var action json.RawMessage
		s.signIn.mu.Lock()
		policy := s.signIn.epoch
		s.signIn.mu.Unlock()
		req := codexbridge.RunRequest{WaitForAvailability: true, Model: choice.Model, Effort: choice.Effort, Prompt: prompt, Instructions: task.Instructions + " Submit one response with " + task.Response.Name + ", then stop.", Tools: []codexbridge.Tool{task.Response}}
		if e = s.prepareAgentContext(&req); e != nil {
			return nil, e
		}
		e = runner.Run(ctx, codexbridge.Owner{ID: task.Owner, Session: ctx, Policy: policy}, req, func(event codexbridge.RunEvent) error {
			if event.Type != "waiting" && event.Type != "admitted" {
				return nil
			}
			if event.Type == "admitted" {
				// A user may have changed the connection while this run waited.
				if err := s.validateBackgroundModelConnection(ctx, choice); err != nil {
					return err
				}
			}
			if availability != nil {
				return availability(event.Type == "waiting")
			}
			return nil
		}, func(_ context.Context, call codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
			if call.Name != task.Response.Name || len(action) > 0 || len(call.Arguments) > backgroundModelResponseBytes {
				return codexbridge.ToolAnswer{Text: "Only one bounded response is accepted.", IsError: true}, nil
			}
			if e := task.Validate(call.Arguments); e != nil {
				return codexbridge.ToolAnswer{Text: e.Error(), IsError: true}, nil
			}
			action = append(json.RawMessage(nil), call.Arguments...)
			return codexbridge.ToolAnswer{Text: "Response received. Stop this turn now."}, nil
		})
		if e != nil {
			return nil, aiAgentFailure(e)
		}
		if len(action) == 0 {
			return nil, aifailure.New("invalid-response", "model", "unknown")
		}
		if e = s.validateBackgroundModelConnection(ctx, choice); e != nil {
			return nil, e
		}
		return action, nil
	}
	endpoint, keyName, _, e := s.endpointForConnection(request, true)
	if e != nil {
		return nil, e
	}
	stored, e := s.assistant.readKeyNamed(keyName)
	if e != nil || !stored.present {
		return nil, aifailure.Authored("authentication", "model", "not-started", "The AI credential is unavailable.")
	}
	if reason := bindingProblem(stored, endpoint); reason != "" {
		return nil, aifailure.Authored("configuration", "model", "not-started", reason)
	}
	var body map[string]any
	path := ""
	switch endpoint.kind {
	case "openai-compatible":
		path = "chat/completions"
		body = map[string]any{"model": choice.Model, "stream": false, "max_tokens": policy.OutputTokens, "messages": []any{map[string]any{"role": "system", "content": task.Instructions}, map[string]any{"role": "user", "content": prompt}}}
		if choice.Effort != "" {
			body["reasoning_effort"] = choice.Effort
		}
	case "anthropic":
		path = "v1/messages"
		body = map[string]any{"model": choice.Model, "max_tokens": policy.OutputTokens, "system": task.Instructions, "messages": []any{map[string]any{"role": "user", "content": prompt}}}
	case "gemini":
		if strings.ContainsAny(choice.Model, "/:\\") {
			return nil, aifailure.Authored("configuration", "model", "not-started", "Unsupported model ID for this API.")
		}
		path = "v1beta/models/" + choice.Model + ":generateContent"
		body = map[string]any{"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": task.Instructions}}}, "contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": policy.OutputTokens, "responseMimeType": "application/json"}}
	default:
		return nil, aifailure.Authored("configuration", "model", "not-started", "This AI protocol does not support background model operations.")
	}
	target, e := relayTarget(endpoint.url, path, "")
	if e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(body)
	raw, e = s.prepareModelContext(endpoint.kind, path, raw)
	if e != nil {
		return nil, e
	}
	outbound, e := http.NewRequestWithContext(ctx, "POST", target.String(), bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	header, value, ok := credentialHeader(endpoint.kind, stored.key)
	if !ok {
		return nil, aifailure.Authored("configuration", "model", "not-started", "Unsupported credential protocol.")
	}
	outbound.Header.Set(header, value)
	outbound.Header.Set("Content-Type", "application/json")
	if endpoint.kind == "anthropic" {
		outbound.Header.Set("anthropic-version", "2023-06-01")
	}
	transport := relayTransport
	if transport == nil {
		transport = http.DefaultTransport
	}

	capacityKey := s.modelCapacityKey(endpoint.url, keyName)
	if availability != nil {
		if err := availability(true); err != nil {
			return nil, err
		}
	}
	release, e := modelCapacity.Acquire(ctx, capacityKey)
	if e != nil {
		return nil, e
	}
	defer release()
	if e = s.validateBackgroundModelConnection(ctx, choice); e != nil {
		return nil, e
	}
	stored, e = s.assistant.readKeyNamed(keyName)
	if e != nil || !stored.present {
		return nil, aifailure.Authored("authentication", "model", "not-started", "The AI credential is unavailable.")
	}
	if reason := bindingProblem(stored, endpoint); reason != "" {
		return nil, aifailure.Authored("configuration", "model", "not-started", reason)
	}
	header, value, ok = credentialHeader(endpoint.kind, stored.key)
	if !ok {
		return nil, aifailure.Authored("configuration", "model", "not-started", "Unsupported credential protocol.")
	}
	outbound.Header.Set(header, value)
	if availability != nil {
		if err := availability(false); err != nil {
			return nil, err
		}
	}
	response, e := transport.RoundTrip(outbound)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var timeout net.Error
		if errors.As(e, &timeout) && timeout.Timeout() {
			return nil, aifailure.New("timeout", "model", "unknown")
		}
		return nil, aifailure.New("connection-lost", "model", "unknown")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		modelCapacity.Throttle(capacityKey)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failureBody, _ := io.ReadAll(io.LimitReader(response.Body, 16385))
		return nil, aifailure.HTTPResponse(response.StatusCode, response.Header, failureBody, "model", time.Now())
	}
	raw, e = readBounded(response.Body, 2<<20)
	if e != nil {
		return nil, aifailure.New("invalid-response", "model", "unknown")
	}
	if bytes.Contains(raw, []byte(stored.key)) {
		return nil, aifailure.New("invalid-response", "model", "unknown")
	}
	if _, err := backgroundCompletion(endpoint.kind, raw); err != nil {
		return nil, err
	}
	var wire struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return nil, aifailure.New("invalid-response", "model", "unknown")
	}
	text := ""
	switch endpoint.kind {
	case "openai-compatible":
		if len(wire.Choices) == 1 {
			text = wire.Choices[0].Message.Content
		}
	case "anthropic":
		for _, part := range wire.Content {
			if part.Type == "text" {
				text += part.Text
			}
		}
	case "gemini":
		if len(wire.Candidates) == 1 {
			for _, part := range wire.Candidates[0].Content.Parts {
				if !part.Thought {
					text += part.Text
				}
			}
		}
	}
	action := json.RawMessage(strings.TrimSpace(text))
	if len(action) > backgroundModelResponseBytes {
		return nil, aifailure.Authored("invalid-response", "model", "unknown", "The model response exceeded its response limit.")
	}
	if !json.Valid(action) {
		// Retain unstructured output as a JSON string so the journal remains
		// valid and the host can reject it as an action with bounded feedback.
		action, _ = json.Marshal(string(action))
	}
	// Retain malformed actions as completed responses. The host can provide
	// bounded correction feedback without replaying an uncertain request.
	if e = s.validateBackgroundModelConnection(ctx, choice); e != nil {
		return nil, e
	}
	return action, nil
}
