package desk

import (
	"context"
	"encoding/json"
	"net/http"
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
const backgroundModelTimeout = 5 * time.Minute

// A host-authored contract, never supplied directly by browser or model callers.
// Validate gates the completed structured response before domain dispatch.
// The original workflow receipt retains malformed completed responses as well.
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
	keyless := e == nil && !stored.present && endpoint.keyless()
	if e != nil || !stored.present && !keyless {
		return aifailure.Authored("authentication", "model", "not-started", "This AI connection needs its saved API key.")
	}
	if reason := bindingProblem(stored, endpoint); reason != "" && !keyless {
		return aifailure.Authored("configuration", "model", "not-started", reason)
	}
	// API reasoning defaults are kept until a shared server-side mapping exists;
	// never silently reinterpret a native effort as a different protocol's budget.
	if choice.Effort != "" && endpoint.kind != "openai-compatible" {
		return aifailure.Authored("configuration", "model", "not-started", "Use the provider default reasoning setting for this API connection.")
	}
	return nil
}
