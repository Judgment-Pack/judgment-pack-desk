package codexbridge

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var errContract = errors.New("unsupported Codex request or response")
var toolName = regexp.MustCompile(`^desk_[A-Za-z0-9_]{1,59}$`)

// These contracts intentionally contain no executable, cwd, sandbox override,
// provider endpoint, credential, MCP server, or raw upstream RPC capability.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type RunRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	Instructions string `json:"instructions"`
	Tools        []Tool `json:"tools"`
	Effort       string `json:"effort,omitempty"`
	Phase        string `json:"phase,omitempty"`
}

func threadParams(request RunRequest, privateWork string) (map[string]any, error) {
	if request.Model == "" || len(request.Model) > 128 || strings.ContainsAny(request.Model, "\r\n\x00") ||
		len(request.Prompt) == 0 || len(request.Prompt) > 200_000 || len(request.Instructions) > 200_000 || len(request.Tools) > 128 {
		return nil, errContract
	}
	switch request.Effort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, errContract
	}
	if request.Phase != "" && request.Phase != "author" && request.Phase != "critic" {
		return nil, errContract
	}
	tools := make([]map[string]any, 0, len(request.Tools))
	seen := map[string]bool{}
	total := 0
	for _, tool := range request.Tools {
		total += len(tool.InputSchema) + len(tool.Description)
		var schema struct {
			Type string `json:"type"`
		}
		if !toolName.MatchString(tool.Name) || seen[tool.Name] || len(tool.Description) > 8192 || len(tool.InputSchema) > 65536 || total > 1<<20 ||
			json.Unmarshal(tool.InputSchema, &schema) != nil || schema.Type != "object" {
			return nil, errContract
		}
		seen[tool.Name] = true
		tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "inputSchema": json.RawMessage(append([]byte(nil), tool.InputSchema...))})
	}
	return map[string]any{
		"model": request.Model, "modelProvider": "openai", "allowProviderModelFallback": false,
		"cwd": privateWork, "approvalPolicy": "never", "permissions": "jps",
		"environments": []any{}, "selectedCapabilityRoots": []any{}, "runtimeWorkspaceRoots": []string{},
		"ephemeral": true, "experimentalRawEvents": false, "baseInstructions": request.Instructions,
		"dynamicTools": tools,
	}, nil
}

func turnParams(threadID string, request RunRequest) map[string]any {
	params := map[string]any{"threadId": threadID, "permissions": "jps", "environments": []any{},
		"input": []map[string]string{{"type": "text", "text": request.Prompt}}}
	if request.Effort != "" {
		params["effort"] = request.Effort
	}
	return params
}

// AccountState is a closed public status. Account identifiers, email addresses,
// tokens, and unrecognized upstream members are intentionally discarded.
type AccountState struct {
	Authenticated bool   `json:"authenticated"`
	Plan          string `json:"plan,omitempty"`
}

func accountState(data json.RawMessage) (AccountState, error) {
	var wire struct {
		RequiresOpenaiAuth bool `json:"requiresOpenaiAuth"`
		Account            *struct {
			Type     string `json:"type"`
			PlanType string `json:"planType"`
		} `json:"account"`
	}
	if json.Unmarshal(data, &wire) != nil || !wire.RequiresOpenaiAuth {
		return AccountState{}, errContract
	}
	if wire.Account == nil {
		return AccountState{}, nil
	}
	if wire.Account.Type != "chatgpt" {
		return AccountState{}, errContract
	}
	plan := ""
	switch wire.Account.PlanType {
	case "free", "go", "plus", "pro", "team", "business", "enterprise", "edu":
		plan = wire.Account.PlanType
	}
	return AccountState{Authenticated: true, Plan: plan}, nil
}

// LoginChallenge is transient UI data, never a persisted configuration value.
// It contains no access/refresh tokens and permits only pinned official origins.
type LoginChallenge struct {
	Method string `json:"method"`
	ID     string `json:"id"`
	URL    string `json:"url"`
	Code   string `json:"code,omitempty"`
}

func loginParams(method string) (map[string]string, error) {
	switch method {
	case "browser":
		return map[string]string{"type": "chatgpt"}, nil
	case "device":
		return map[string]string{"type": "chatgptDeviceCode"}, nil
	default:
		return nil, errContract
	}
}

func loginChallenge(data json.RawMessage) (LoginChallenge, error) {
	var wire struct {
		Type            string `json:"type"`
		ID              string `json:"loginId"`
		AuthURL         string `json:"authUrl"`
		VerificationURL string `json:"verificationUrl"`
		Code            string `json:"userCode"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.ID == "" || len(wire.ID) > 256 {
		return LoginChallenge{}, errContract
	}
	challenge := LoginChallenge{ID: wire.ID}
	switch wire.Type {
	case "chatgpt":
		challenge.Method = "browser"
		challenge.URL = wire.AuthURL
	case "chatgptDeviceCode":
		challenge.Method = "device"
		challenge.URL = wire.VerificationURL
		challenge.Code = wire.Code
	default:
		return LoginChallenge{}, errContract
	}
	u, err := url.Parse(challenge.URL)
	if err != nil || len(challenge.URL) > 8192 || u.Scheme != "https" || u.Host != "auth.openai.com" || u.User != nil || u.Fragment != "" {
		return LoginChallenge{}, errContract
	}
	if challenge.Method == "browser" {
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return LoginChallenge{}, errContract
		}
		for key, values := range query {
			if len(values) != 1 {
				return LoginChallenge{}, errContract
			}
			switch key {
			case "response_type", "client_id", "redirect_uri", "scope", "code_challenge", "code_challenge_method", "id_token_add_organizations", "codex_cli_simplified_flow", "state", "originator", "allowed_workspace_id":
			default:
				return LoginChallenge{}, errContract
			}
		}
		if u.Path != "/oauth/authorize" {
			return LoginChallenge{}, errContract
		}
	} else if u.Path != "/codex/device" || u.RawQuery != "" || len(challenge.Code) == 0 || len(challenge.Code) > 64 || strings.ContainsAny(challenge.Code, "\r\n\x00") {
		return LoginChallenge{}, errContract
	}
	return challenge, nil
}
