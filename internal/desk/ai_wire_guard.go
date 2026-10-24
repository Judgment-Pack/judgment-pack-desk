package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/aicontext"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// Wire guards inspect only the model/output budget and total serialized bytes.
// Adapters own all prompt layers through Host; guards never read brief/memory
// sources, infer a recall query, append instructions, reserialize or clip input.
func (s *Server) wireContextPolicy(model string) (aicontext.Policy, error) {
	profile, err := s.readAssistantProfile()
	if err != nil {
		return aicontext.Policy{}, err
	}
	return aicontext.Resolve(profile.Context, model), nil
}

func (s *Server) checkAgentWireContext(request codexbridge.RunRequest) error {
	policy, err := s.wireContextPolicy(request.Model)
	if err != nil {
		return err
	}
	tools, err := json.Marshal(request.Tools)
	if err != nil {
		return err
	}
	return policy.Check(request.Prompt, request.Instructions, string(tools))
}

func (s *Server) checkModelWireContext(kind, suffix string, raw []byte) error {
	var body map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil || body == nil {
		// Opaque legacy probes still have a byte ceiling; they cannot cause
		// prompt injection or a source read even when a brief is configured.
		policy, err := s.wireContextPolicy("")
		if err != nil {
			return err
		}
		return policy.Check(string(raw))
	}
	model, _ := body["model"].(string)
	if kind == "gemini" {
		model, _, _ = strings.Cut(strings.TrimPrefix(suffix, "v1beta/models/"), ":")
	}
	policy, err := s.wireContextPolicy(model)
	if err != nil {
		return err
	}
	output := policy.OutputTokens
	number := func(value any) {
		if n, ok := value.(json.Number); ok {
			v, e := n.Int64()
			if e == nil && v > int64(output) && v <= 2000000 {
				output = int(v)
			}
		}
	}
	number(body["max_tokens"])
	number(body["max_completion_tokens"])
	if generation, ok := body["generationConfig"].(map[string]any); ok {
		number(generation["maxOutputTokens"])
	}
	if output > policy.ContextWindowTokens/2 {
		return errors.New("The AI output budget leaves too little room for context.")
	}
	policy.OutputTokens = output
	return policy.Check(string(raw))
}
