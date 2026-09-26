package codexbridge

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Model is a closed discovery result. Native availability prose, account data,
// service-tier upsells and provider overrides never reach the browser.
type Model struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Efforts       []string `json:"efforts"`
	DefaultEffort string   `json:"defaultEffort"`
}

func supportedEffort(value string) bool {
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}
func (m *Manager) Models(ctx context.Context) ([]Model, error) {
	if !m.mu.TryLock() {
		return nil, ErrBusy
	}
	defer m.mu.Unlock()
	if m.closed || m.disconnecting || m.active != nil || m.pending != nil {
		return nil, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx, release := m.operation(ctx)
	defer release()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	account, err := m.readAccount(ctx, true)
	if err != nil {
		return nil, err
	}
	if !account.Authenticated {
		return nil, ErrSignIn
	}
	return m.models(ctx)
}
func (m *Manager) models(ctx context.Context) ([]Model, error) {
	result := []Model{}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var response struct {
			Data []json.RawMessage `json:"data"`
			Next *string           `json:"nextCursor"`
		}
		if err := m.client.call(ctx, "model/list", params, &response); err != nil {
			return nil, err
		}
		if response.Data == nil || len(response.Data) > 100 {
			return nil, ErrRun
		}
		for _, raw := range response.Data {
			var model struct {
				Model   string `json:"model"`
				Name    string `json:"displayName"`
				Hidden  bool   `json:"hidden"`
				Default string `json:"defaultReasoningEffort"`
				Efforts []struct {
					Value string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			}
			if json.Unmarshal(raw, &model) != nil || !validID(model.Model) || len(model.Model) > 128 || len(model.Name) > 256 || strings.ContainsAny(model.Name, "\r\n\x00") {
				return nil, ErrRun
			}
			if model.Hidden {
				continue
			}
			if seen[model.Model] {
				return nil, ErrRun
			}
			seen[model.Model] = true
			item := Model{ID: model.Model, Name: model.Name, Efforts: []string{}}
			efforts := map[string]bool{}
			for _, option := range model.Efforts {
				if supportedEffort(option.Value) && !efforts[option.Value] {
					item.Efforts = append(item.Efforts, option.Value)
					efforts[option.Value] = true
				}
			}
			// Refuse an implicit native default outside the constrained effort set.
			if !efforts[model.Default] {
				continue
			}
			item.DefaultEffort = model.Default
			if item.Name == "" {
				item.Name = item.ID
			}
			result = append(result, item)
		}
		if response.Next == nil || *response.Next == "" {
			return result, nil
		}
		cursor = *response.Next
		if len(cursor) > 2048 || cursors[cursor] {
			return nil, ErrRun
		}
		cursors[cursor] = true
	}
	return nil, ErrLimit
}
