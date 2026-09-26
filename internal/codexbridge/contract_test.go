package codexbridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoEnvironmentContract(t *testing.T) {
	request := RunRequest{Model: "gpt-5.5", Prompt: "Check this pack", Instructions: "Use Desk tools", Tools: []Tool{{Name: "desk_validate", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	params, err := threadParams(request, "/private/empty")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(params)
	var roundTrip map[string]any
	_ = json.Unmarshal(encoded, &roundTrip)
	for _, key := range []string{"environments", "runtimeWorkspaceRoots", "selectedCapabilityRoots"} {
		if items, ok := roundTrip[key].([]any); !ok || len(items) != 0 {
			t.Errorf("%s must be a non-null empty array", key)
		}
	}
	if params["modelProvider"] != "openai" || params["allowProviderModelFallback"] != false || params["approvalPolicy"] != "never" || params["permissions"] != "jps" {
		t.Fatal("weakened provider or execution contract")
	}
	turn, _ := json.Marshal(turnParams("thread", request))
	if !strings.Contains(string(turn), `"environments":[]`) {
		t.Fatal("turn restored a local environment")
	}
	for _, name := range []string{"apply_patch", "view_image", "exec_command", "skills.read", "desk_"} {
		request.Tools[0].Name = name
		if _, err := threadParams(request, "/private/empty"); err == nil {
			t.Errorf("native or invalid tool name accepted: %s", name)
		}
	}
	request.Tools = []Tool{{Name: "desk_x", InputSchema: json.RawMessage(`{"type":"object"}`)}, {Name: "desk_x", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	if _, err := threadParams(request, "/private/empty"); err == nil {
		t.Fatal("duplicate tools accepted")
	}
}

func TestAccountIsSubscriptionOnlyAndSanitized(t *testing.T) {
	account, err := accountState(json.RawMessage(`{"requiresOpenaiAuth":true,"account":{"type":"chatgpt","planType":"plus","email":"PRIVATE_EMAIL","accessToken":"PRIVATE_TOKEN"}}`))
	if err != nil || !account.Authenticated || account.Plan != "plus" {
		t.Fatalf("valid account: %#v %v", account, err)
	}
	encoded, _ := json.Marshal(account)
	if strings.Contains(string(encoded), "PRIVATE") {
		t.Fatal("private account data escaped")
	}
	for _, body := range []string{`{"requiresOpenaiAuth":false}`, `{"requiresOpenaiAuth":true,"account":{"type":"apiKey"}}`, `{"requiresOpenaiAuth":true,"account":{"type":"chatgptAuthTokens"}}`, `{}`} {
		if _, err := accountState(json.RawMessage(body)); err == nil {
			t.Fatalf("accepted wrong authentication: %s", body)
		}
	}
	empty, err := accountState(json.RawMessage(`{"requiresOpenaiAuth":true,"account":null}`))
	if err != nil || empty.Authenticated {
		t.Fatal("absent account treated as connected")
	}
}

func TestLoginMethodsAndOrigins(t *testing.T) {
	for _, method := range []string{"apiKey", "chatgptAuthTokens", "amazonBedrock", ""} {
		if _, err := loginParams(method); err == nil {
			t.Errorf("method accepted: %s", method)
		}
	}
	for _, method := range []string{"browser", "device"} {
		if _, err := loginParams(method); err != nil {
			t.Errorf("method rejected: %s", method)
		}
	}
	for _, url := range []string{"https://auth.openai.com/oauth/authorize?state=transient"} {
		data, _ := json.Marshal(map[string]string{"type": "chatgpt", "loginId": "id", "authUrl": url, "accessToken": "PRIVATE_TOKEN"})
		challenge, err := loginChallenge(data)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(challenge)
		if strings.Contains(string(encoded), "PRIVATE") {
			t.Fatal("token escaped challenge")
		}
	}
	for _, url := range []string{"https://auth.openai.com/oauth/authorize?access_token=PRIVATE_TOKEN", "https://auth.openai.com/oauth/authorize?state=one&state=two", "http://auth.openai.com/authorize", "https://auth.openai.com.evil/authorize", "https://auth.openai.com@evil/authorize", "https://auth.openai.com/other", "https://auth.openai.com:443/authorize", "https://auth.openai.com/authorize#fragment"} {
		data, _ := json.Marshal(map[string]string{"type": "chatgpt", "loginId": "id", "authUrl": url})
		if _, err := loginChallenge(data); err == nil {
			t.Errorf("URL accepted: %s", url)
		}
	}
	challenge, err := loginChallenge(json.RawMessage(`{"type":"chatgptDeviceCode","loginId":"id","verificationUrl":"https://auth.openai.com/codex/device","userCode":"ABCD-EFGH"}`))
	if err != nil || challenge.Method != "device" || challenge.Code != "ABCD-EFGH" {
		t.Fatalf("device login: %#v %v", challenge, err)
	}
}

func TestToolSchemaPreservesNumericLiterals(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"const":9007199254740993},"fraction":{"enum":[0.1234567890123456789,1e400]}}}`)
	params, err := threadParams(RunRequest{Model: "model", Prompt: "prompt", Tools: []Tool{{Name: "desk_check", InputSchema: schema}}}, "/private/work")
	if err != nil {
		t.Fatal(err)
	}
	// The caller can reuse its request buffer after admission.
	schema[0] = ' '
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{"9007199254740993", "0.1234567890123456789", "1e400"} {
		if !strings.Contains(string(encoded), literal) {
			t.Fatalf("schema changed %s: %s", literal, encoded)
		}
	}
	for _, invalid := range []string{`null`, `[]`, `{"type":"array"}`, `{"type":"object"}{}`, `{"type":"object","const":NaN}`} {
		_, err := threadParams(RunRequest{Model: "model", Prompt: "prompt", Tools: []Tool{{Name: "desk_check", InputSchema: json.RawMessage(invalid)}}}, "/private/work")
		if err == nil {
			t.Fatalf("accepted invalid schema: %s", invalid)
		}
	}
}
