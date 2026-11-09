package codexbridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Hand-authored in-memory replies exercise call's classification without a
// native process, network listener, account profile or provider text escaping.
type protocolErrorInput func([]byte) (int, error)

func (f protocolErrorInput) Write(b []byte) (int, error) { return f(b) }
func (f protocolErrorInput) Close() error                { return nil }

func protocolErrorClient(t *testing.T, envelope string) *client {
	t.Helper()
	c := &client{pending: make(map[string]chan frame), done: make(chan struct{})}
	c.input = protocolErrorInput(func(b []byte) (int, error) {
		var sent frame
		if err := json.Unmarshal(b, &sent); err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		answer := c.pending[string(sent.ID)]
		c.mu.Unlock()
		if answer == nil {
			t.Fatal("request has no registered reply")
		}
		reply := frame{ID: sent.ID, Error: json.RawMessage(envelope)}
		if envelope == "" || envelope == "null" {
			reply.Result = json.RawMessage(`{"value":42}`)
		}
		answer <- reply
		return len(b), nil
	})
	return c
}

func TestProtocolErrorEnvelopeClassification(t *testing.T) {
	for _, tc := range []struct {
		name, envelope string
		valid          bool
	}{
		{"string", `"PRIVATE_SENTINEL"`, false},
		{"array", `[]`, false},
		{"boolean", `false`, false},
		{"number", `42`, false},
		{"empty-object", `{}`, false},
		{"missing-code", `{"message":"PRIVATE_SENTINEL"}`, false},
		{"missing-message", `{"code":-1}`, false},
		{"wrong-code-case", `{"Code":-1,"message":"PRIVATE_SENTINEL"}`, false},
		{"wrong-message-case", `{"code":-1,"Message":"PRIVATE_SENTINEL"}`, false},
		{"null-code", `{"code":null,"message":"PRIVATE_SENTINEL"}`, false},
		{"null-message", `{"code":-1,"message":null}`, false},
		{"string-code", `{"code":"PRIVATE_SENTINEL","message":"PRIVATE_SENTINEL"}`, false},
		{"fractional-code", `{"code":-1.5,"message":"PRIVATE_SENTINEL"}`, false},
		{"boolean-code", `{"code":false,"message":"PRIVATE_SENTINEL"}`, false},
		{"number-message", `{"code":-1,"message":42}`, false},
		{"boolean-message", `{"code":-1,"message":false}`, false},
		{"object-message", `{"code":-1,"message":{"private":"PRIVATE_SENTINEL"}}`, false},
		{"wrong-field-types", `{"code":"PRIVATE_SENTINEL","message":false}`, false},
		{"backend", `{"code":-1,"message":"PRIVATE_SENTINEL"}`, true},
		{"backend-data", `{"code":-32000,"message":"PRIVATE_SENTINEL","data":{"private":"PRIVATE_SENTINEL"}}`, true},
		{"empty-message", `{"code":0,"message":""}`, true},
	} {
		for _, method := range []string{"account/read", "model/list"} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				c := protocolErrorClient(t, tc.envelope)
				var result json.RawMessage
				err := c.call(context.Background(), method, nil, &result)
				want := ErrUnavailable
				if tc.valid {
					want = ErrUpstream
				}
				if !errors.Is(err, want) || errors.Is(err, ErrAccountLookup) != (tc.valid && method == "account/read") {
					t.Fatalf("wrong RPC error classification: %v, want %v (lookup %t)", err, want, tc.valid && method == "account/read")
				}
				if !tc.valid && errors.Is(err, ErrUpstream) {
					t.Fatalf("malformed RPC error became backend failure: %v", err)
				}
				if strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
					t.Fatal("native error text escaped")
				}
			})
		}
	}
}

func TestProtocolErrorEnvelopeSuccessControls(t *testing.T) {
	for _, envelope := range []string{"", "null"} {
		c := protocolErrorClient(t, envelope)
		var result struct {
			Value int `json:"value"`
		}
		if err := c.call(context.Background(), "account/read", nil, &result); err != nil || result.Value != 42 {
			t.Fatalf("successful result changed: %+v, %v", result, err)
		}
	}
}
