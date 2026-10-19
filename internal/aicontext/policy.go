// Package aicontext owns Desk's provider-independent context limits. The JSON
// defaults are also imported by the browser; neither adapter owns another table.
package aicontext

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aifailure"
	"io"
	"strings"
	"unicode/utf8"
)

//go:embed policy.json
var defaultsJSON []byte

type Policy struct {
	MemoryBytes         int `json:"memoryBytes"`
	Version             int `json:"version"`
	MaxInputBytes       int `json:"maxInputBytes"`
	ContextWindowTokens int `json:"contextWindowTokens"`
	OutputTokens        int `json:"outputTokens"`
	ToolResultBytes     int `json:"toolResultBytes"`
	ReadBytes           int `json:"readBytes"`
	RetainedBytes       int `json:"retainedBytes"`
	RecentMessages      int `json:"recentMessages"`
	HistoryBytes        int `json:"historyBytes"`
	BriefBytes          int `json:"briefBytes"`
}
type ModelBudget struct {
	ContextWindowTokens int `json:"contextWindowTokens"`
	OutputTokens        int `json:"outputTokens"`
}
type Configuration struct {
	Version      int                    `json:"version"`
	ProjectBrief string                 `json:"projectBrief,omitempty"`
	Models       map[string]ModelBudget `json:"models,omitempty"`
}

func Defaults() Policy { var p Policy; _ = json.Unmarshal(defaultsJSON, &p); return p }
func Decode(data []byte) (*Configuration, error) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return nil, errors.New("invalid assistant context configuration")
	}
	if value, present := raw["projectBrief"]; present && (string(value) == "null" || string(value) == `""`) {
		return nil, errors.New("project brief must be a relative project file")
	}
	if value, present := raw["models"]; present && string(value) == "null" {
		return nil, errors.New("invalid assistant model context budget")
	}
	var c Configuration
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || c.Version != 1 || len(c.Models) > 128 {
		return nil, errors.New("invalid assistant context configuration")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid assistant context configuration")
	}
	if c.ProjectBrief != "" && (len(c.ProjectBrief) > 512 || strings.ContainsAny(c.ProjectBrief, "\\\x00\r\n") || strings.HasPrefix(c.ProjectBrief, "/") || strings.Contains(c.ProjectBrief, ":") || badPath(c.ProjectBrief)) {
		return nil, errors.New("project brief must be a relative project file")
	}
	for name, b := range c.Models {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 128 || strings.ContainsAny(name, "\r\n\x00") || b.ContextWindowTokens < 4096 || b.ContextWindowTokens > 2000000 || b.OutputTokens < 1024 || b.OutputTokens > b.ContextWindowTokens/2 {
			return nil, errors.New("invalid assistant model context budget")
		}
	}
	return &c, nil
}
func badPath(path string) bool {
	for _, p := range strings.Split(path, "/") {
		if p == "" || p == "." || p == ".." {
			return true
		}
	}
	return false
}
func Resolve(c *Configuration, model string) Policy {
	p := Defaults()
	if c != nil {
		if b, ok := c.Models[model]; ok {
			p.ContextWindowTokens = b.ContextWindowTokens
			p.OutputTokens = b.OutputTokens
		}
	}
	return p
}

// Estimates are explicitly estimates, never claims about provider token usage.
// Keep a fixed transport ceiling even for models with larger context windows.
func (p Policy) InputBytes() int {
	return min(p.MaxInputBytes, max(1024, (p.ContextWindowTokens-p.OutputTokens)*3))
}
func (p Policy) Check(parts ...string) error {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	if total > p.InputBytes() {
		return aifailure.New("context-limit", "context", "not-started")
	}
	return nil
}
func Prefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}
