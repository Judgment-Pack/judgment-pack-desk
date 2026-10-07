package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const assistantProfilePath = "jpack-assistant.json"
const assistantProfileProblem = "Desk model preferences could not be read. Fix jpack-assistant.json or reload it in Admin › Assistant."
const inheritedAssistantProfile = "{\n  \"profileVersion\": 1,\n  \"codex\": { \"inherit\": true },\n  \"api\": { \"inherit\": true }\n}\n"

type modelPreferences struct {
	Inherit  bool     `json:"inherit"`
	Models   []string `json:"models,omitempty"`
	Model    *string  `json:"model,omitempty"`
	Effort   string   `json:"effort,omitempty"`
	Thinking string   `json:"thinking,omitempty"`
}
type assistantProfile struct {
	Version           int                          `json:"profileVersion"`
	Codex             *modelPreferences            `json:"codex,omitempty"`
	API               *modelPreferences            `json:"api,omitempty"`
	Inherit           *bool                        `json:"inherit,omitempty"`
	Connections       []string                     `json:"connections,omitempty"`
	DefaultConnection *string                      `json:"defaultConnection,omitempty"`
	Models            map[string]*modelPreferences `json:"models,omitempty"`
}

func decodeAssistantProfile(data []byte) (*assistantProfile, error) {
	fail := func() (*assistantProfile, error) { return nil, errors.New(assistantProfileProblem) }
	if len(data) > 65536 || !validUTF8(data) {
		return fail()
	}
	var value map[string]any
	if json.Unmarshal(data, &value) == nil && value != nil && value["profileVersion"] == float64(2) {
		return decodeAssistantProfileV2(data, value)
	}
	if json.Unmarshal(data, &value) != nil || value == nil || value["profileVersion"] != float64(1) {
		return fail()
	}
	for key := range value {
		if !contains([]string{"profileVersion", "codex", "api"}, key) {
			return fail()
		}
	}
	for _, key := range []string{"codex", "api"} {
		raw, present := value[key]
		if !present {
			continue
		}
		row, ok := raw.(map[string]any)
		if !ok {
			return fail()
		}
		inherit, ok := row["inherit"].(bool)
		if !ok {
			return fail()
		}
		keys := []string{"inherit"}
		if !inherit {
			keys = append(keys, "model", "models")
			if key == "codex" {
				keys = append(keys, "effort")
			} else {
				keys = append(keys, "thinking")
			}
		}
		for name := range row {
			if !contains(keys, name) {
				return fail()
			}
		}
		if inherit {
			continue
		}
		models, ok := row["models"].([]any)
		if !ok || len(models) > 128 {
			return fail()
		}
		seen := map[string]bool{}
		for _, v := range models {
			id, ok := v.(string)
			if !ok || id == "" || strings.TrimSpace(id) != id || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") || seen[id] {
				return fail()
			}
			seen[id] = true
		}
		rawModel, present := row["model"]
		if !present {
			return fail()
		}
		if len(models) == 0 {
			if rawModel != nil {
				return fail()
			}
		} else {
			model, ok := rawModel.(string)
			if !ok || !seen[model] {
				return fail()
			}
		}
		if v, present := row["effort"]; present {
			e, ok := v.(string)
			if !ok || !contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, e) {
				return fail()
			}
		}
		if v, present := row["thinking"]; present {
			e, ok := v.(string)
			if !ok || !contains(AssistantThinkingTiers, e) {
				return fail()
			}
		}
	}
	var result assistantProfile
	if json.Unmarshal(data, &result) != nil {
		return fail()
	}
	return &result, nil
}
func (s *Server) readAssistantProfile() (*assistantProfile, error) {
	if err := s.refuseSymlinkedPath(assistantProfilePath); err != nil {
		return nil, errors.New(assistantProfileProblem)
	}
	data, status, err := s.readThroughRootWithin(assistantProfilePath, 65536)
	if err != nil {
		if status == http.StatusNotFound {
			return &assistantProfile{Version: 1}, nil
		}
		return nil, errors.New(assistantProfileProblem)
	}
	return decodeAssistantProfile(data)
}

// Inspect only the model selector. The original body is forwarded unchanged.
func profileAPIModelAllowed(kind, suffix string, body []byte, models []string) bool {
	if kind == "gemini" {
		const prefix = "v1beta/models/"
		if !strings.HasPrefix(suffix, prefix) {
			return false
		}
		id, method, ok := strings.Cut(strings.TrimPrefix(suffix, prefix), ":")
		return ok && contains([]string{"generateContent", "streamGenerateContent", "countTokens"}, method) && contains(models, id)
	}
	model, ok := topLevelModel(body)
	return ok && contains(models, model)
}

// topLevelModel is a request's one top-level `model` member, read the way a
// provider reads it: by its exact name. A second member whose name is
// `model` in any case is refused rather than chosen between, because Go's
// decoder matches names without case and keeps the last, and a provider that
// reads the exact name, or the first, would run a model nobody allowed.
func topLevelModel(body []byte) (string, bool) {
	d := json.NewDecoder(bytes.NewReader(body))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return "", false
	}
	var model json.RawMessage
	found := 0
	for d.More() {
		t, err := d.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return "", false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return "", false
		}
		if strings.EqualFold(key, "model") {
			found++
			if key != "model" {
				return "", false
			}
			model = value
		}
	}
	// One whole object and nothing after it: the bytes the provider is sent
	// are the bytes read here.
	if t, err := d.Token(); err != nil || t != json.Delim('}') {
		return "", false
	}
	if _, err := d.Token(); err != io.EOF {
		return "", false
	}
	if found != 1 {
		return "", false
	}
	var id string
	if json.Unmarshal(model, &id) != nil {
		return "", false
	}
	return id, true
}

func decodeAssistantProfileV2(data []byte, value map[string]any) (*assistantProfile, error) {
	fail := func() (*assistantProfile, error) { return nil, errors.New(assistantProfileProblem) }
	if !uniqueJSON(data) {
		return fail()
	}
	inherit, ok := value["inherit"].(bool)
	if !ok {
		return fail()
	}
	keys := []string{"profileVersion", "inherit"}
	if !inherit {
		keys = append(keys, "connections", "defaultConnection", "models")
	}
	for k := range value {
		if !contains(keys, k) {
			return fail()
		}
	}
	var p assistantProfile
	if json.Unmarshal(data, &p) != nil {
		return fail()
	}
	if inherit {
		return &p, nil
	}
	if _, present := value["defaultConnection"]; !present {
		return fail()
	}
	if raw, present := value["models"]; present {
		if object, ok := raw.(map[string]any); !ok || object == nil {
			return fail()
		}
	}
	if p.Connections == nil || len(p.Connections) > 32 || len(p.Models) > 32 {
		return fail()
	}
	seen := map[string]bool{}
	for _, id := range p.Connections {
		if !aiIDPattern.MatchString(id) || seen[id] {
			return fail()
		}
		seen[id] = true
	}
	if len(seen) == 0 {
		if p.DefaultConnection != nil {
			return fail()
		}
	} else if p.DefaultConnection == nil || !seen[*p.DefaultConnection] {
		return fail()
	}
	for id, prefs := range p.Models {
		if !seen[id] || prefs == nil {
			return fail()
		}
		rows, ok := value["models"].(map[string]any)
		if !ok {
			return fail()
		}
		row, ok := rows[id].(map[string]any)
		if !ok {
			return fail()
		}
		key := "api"
		if _, ok := row["effort"]; ok {
			key = "codex"
		}
		raw, _ := json.Marshal(map[string]any{"profileVersion": 1, key: row})
		if _, err := decodeAssistantProfile(raw); err != nil {
			return fail()
		}
	}
	return &p, nil
}
