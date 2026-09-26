package desk

import "strings"

// An optional inactive target may be retained when switching back to API access.
// No credentials, executable, endpoint or native configuration are accepted.
type AssistantAgentConfig struct {
	Provider   string   `json:"provider"`
	AuthMethod string   `json:"authMethod"`
	Model      *string  `json:"model"`
	Tools      []string `json:"tools"`
	Effort     string   `json:"effort,omitempty"`
}

func decodeAssistantAgent(value any) (*AssistantAgentConfig, []deskProblem) {
	a, problems := object(value, "assistant.agent", []string{"provider", "authMethod", "model", "tools", "effort"})
	if a == nil {
		return nil, problems
	}
	bad := func(key, reason string) {
		problems = append(problems, deskProblem{Key: "assistant.agent." + key, Reason: reason})
	}
	if a["provider"] != "openai" {
		bad("provider", "must be openai")
	}
	if a["authMethod"] != "subscription" {
		bad("authMethod", "must be subscription")
	}
	result := &AssistantAgentConfig{Provider: "openai", AuthMethod: "subscription", Tools: []string{}}
	if a["model"] != nil {
		model, ok := a["model"].(string)
		if !ok || strings.TrimSpace(model) == "" || len(model) > 128 || strings.ContainsAny(model, "\r\n\x00") {
			bad("model", "must be a non-empty model ID of at most 128 characters, or null")
		} else {
			model = strings.TrimSpace(model)
			result.Model = &model
		}
	}
	values, ok := a["tools"].([]any)
	if ok {
		for _, value := range values {
			name, isString := value.(string)
			if !isString || !contains(AssistantTools, name) {
				ok = false
				break
			}
			result.Tools = append(result.Tools, name)
		}
	}
	if !ok {
		bad("tools", "must be an explicit array of allowed assistant tools")
	}
	if effort, present := a["effort"]; present {
		named, ok := effort.(string)
		if !ok || !contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, named) {
			bad("effort", "must be a supported Codex reasoning effort")
		} else {
			result.Effort = named
		}
	}
	return result, problems
}
