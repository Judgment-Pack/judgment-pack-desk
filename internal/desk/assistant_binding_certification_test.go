package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/delegate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/wire"
	"github.com/Judgment-Pack/judgment-pack-agent/go/certification"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

var assistantCertificationPhysical atomic.Int64

// A canonical typed model timeout specifies classification, not connection loss.
// Represent exactly that fixture value as a complete, ordinary HTTP504 response:
// failure.Normalize preserves timeout/model/unknown and adds the known status.
// Plain errors and errors with additional provenance remain physical interruptions.
func assistantCertificationTimeoutReply(w http.ResponseWriter, err error) bool {
	typed, ok := err.(*failure.Error)
	if !ok || !reflect.DeepEqual(typed.Failure, failure.New("timeout", "model", "unknown")) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGatewayTimeout)
	_, _ = io.WriteString(w, `{"error":{"message":"Fixture model timeout"}}`)
	return true
}

func TestAssistantBindingCertificationTimeoutReply(t *testing.T) {
	for _, err := range []error{errors.New("Interrupted after edit"), context.DeadlineExceeded, fmt.Errorf("wrapped: %w", failure.Err("timeout", "model", "unknown")), failure.Err("unavailable", "model", "unknown"), failure.Err("timeout", "agent", "unknown")} {
		recorder := httptest.NewRecorder()
		if assistantCertificationTimeoutReply(recorder, err) || recorder.Body.Len() != 0 || recorder.Flushed {
			t.Fatalf("noncanonical error received a complete response: %T", err)
		}
	}
	recorder := httptest.NewRecorder()
	original := failure.Err("timeout", "model", "unknown")
	if !assistantCertificationTimeoutReply(recorder, original) {
		t.Fatal("canonical timeout was not represented")
	}
	result := recorder.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	assistantRunMust(t, err)
	got := failure.Normalize(&wire.RelayRefusal{Status: result.StatusCode, Body: string(body), Headers: result.Header}, "model", time.Unix(0, 0))
	want := original.Failure
	status := int64(http.StatusGatewayTimeout)
	want.Status = &status
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HTTP timeout normalization changed more than known status: got %#v, want %#v", got, want)
	}
}

// The released suite is a read-only input at the locked source revision. The
// ordinary package tests do not need its Node fixture; the required dev check
// provides it and runs this test explicitly.
func assistantBindingCertificationServer(t *testing.T) *certification.Server {
	t.Helper()
	source := os.Getenv("DESK_ASSISTANT_CERTIFICATION_SOURCE")
	if source == "" {
		t.Skip("run scripts/dev/assistant-binding-certification.sh with the locked source fixture")
	}
	previous, err := os.Getwd()
	assistantRunMust(t, err)
	assistantRunMust(t, os.Chdir(source))
	t.Cleanup(func() { assistantRunMust(t, os.Chdir(previous)) })
	server, err := certification.StartServer(t.Context())
	assistantRunMust(t, err)
	t.Cleanup(server.Close)
	return server
}

// Admit a saved, authenticated Desk response with an explicitly saved model
// policy. The protocol fixture only holds the executor open: every call under
// test uses the real relay, credentials, source/currentness and durable receipts.
func assistantBindingCertificationExecution(t *testing.T, endpoint, family, model, critic, child string) *assistantRunExecution {
	t.Helper()
	entered := make(chan *assistantRunExecution, 1)
	s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		entered <- e
		<-ctx.Done()
		return ctx.Err()
	})
	models := []string{model}
	for _, value := range []string{critic, child} {
		if value != "" && !contains(models, value) {
			models = append(models, value)
		}
	}
	connection := aiTestConnection(testAIAlpha, "Certification fixture", endpoint)
	connection.Assistant.Endpoint.Kind = family
	connection.Assistant.Endpoint.Models = models
	connection.Assistant.Endpoint.Model = &model
	connection.Assistant.Endpoint.CriticModel = critic
	connection.Assistant.Endpoint.ChildModel = child
	raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	assistantRunMust(t, err)
	assistantRunMust(t, s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	var chat map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
	chat["model"], err = json.Marshal(model)
	assistantRunMust(t, err)
	doc.Chats[0], err = json.Marshal(chat)
	assistantRunMust(t, err)
	if response := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	saved = policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	start.Revision = digestOf(doc.Chats[0])
	origin, _ := endpointOrigin(endpoint)
	assistantRunMust(t, s.assistant.storeKeyNamed(connectionKeyName(&connection), storedKey{present: true, key: "desk-certification-fixture-key", origin: origin, kind: family}))
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	select {
	case e := <-entered:
		t.Cleanup(func() {
			e.cancel()
			select {
			case <-e.done:
			case <-time.After(3 * time.Second):
				t.Error("certification execution did not drain")
			}
		})
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("certification execution did not start")
	}
	return nil
}

// Each released leg supplies its scripted Model capability. A normal owned
// HTTP endpoint uses that capability as its backend, preserving the suite's
// body/seam observations. The adapter first crosses the physical Desk relay
// endpoint, using the saved private key. No certification internals are read or
// changed, and no adapter request is rewritten to satisfy the oracle.
func assistantBindingCertificationAPI(t *testing.T, inner adapter.Adapter) adapter.Adapter {
	t.Helper()
	return adapter.Func(func(ctx context.Context, run *adapter.Run) error {
		held := *run
		bind := func(family, model, critic, child string, backend relay.Call) relay.Call {
			if backend == nil {
				return nil
			}
			if family == "" {
				family = "openai-compatible"
			}
			var arrived, interrupted, timeoutReplies atomic.Int32
			type wireRequest struct{ suffix, body string }
			var wireMu sync.Mutex
			physical := map[wireRequest]int{}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				credentialName, credentialValue := "Authorization", "Bearer desk-certification-fixture-key"
				if family == "anthropic" {
					credentialName, credentialValue = "X-Api-Key", "desk-certification-fixture-key"
				}
				if family == "gemini" {
					credentialName, credentialValue = "X-Goog-Api-Key", "desk-certification-fixture-key"
				}
				credentialsOK := r.Header.Get(credentialName) == credentialValue
				for _, name := range []string{"Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key", "Cookie", "Proxy-Authorization"} {
					if name != credentialName && r.Header.Get(name) != "" {
						credentialsOK = false
					}
				}
				if !credentialsOK || r.Header.Get(aiConnectionHeader) != "" || r.Header.Get(aiRevisionHeader) != "" {
					t.Error("actual provider endpoint did not receive only the saved provider credential")
					http.Error(w, "credential boundary", http.StatusForbidden)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					http.Error(w, "body", 500)
					return
				}
				suffix := strings.TrimPrefix(r.URL.RequestURI(), "/fixture/")
				headers := map[string]string{}
				for name, values := range r.Header {
					if len(values) == 1 {
						headers[name] = values[0]
					}
				}
				arrived.Add(1)
				assistantCertificationPhysical.Add(1)
				wireMu.Lock()
				physical[wireRequest{suffix, string(body)}]++
				wireMu.Unlock()
				response, err := backend(r.Context(), suffix, relay.Request{Method: r.Method, Headers: headers, Body: body})
				if err != nil {
					if assistantCertificationTimeoutReply(w, err) {
						timeoutReplies.Add(1)
						return
					}
					// A scripted transport interruption is an upstream connection
					// loss at this physical endpoint, not a test fixture failure.
					interrupted.Add(1)
					panic(http.ErrAbortHandler)
				}
				defer response.Body.Close()
				for name, values := range response.Header {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				w.WriteHeader(response.StatusCode)
				if _, err = io.Copy(w, response.Body); err != nil {
					t.Errorf("scripted response: %v", err)
				}
			}))
			t.Cleanup(provider.Close)
			e := assistantBindingCertificationExecution(t, provider.URL+"/fixture", family, model, critic, child)
			call, err := e.assistantModel()
			assistantRunMust(t, err)
			t.Cleanup(func() {
				record := assistantExecutionRecord(t, e)
				dispatched, uncertain := 0, 0
				for _, op := range record.Operations {
					if op.Kind == "model" && contains([]string{"completed", "dispatched", "uncertain"}, op.State) {
						dispatched++
						if op.State != "completed" {
							uncertain++
						}
						var intent struct {
							Suffix  string
							Request relay.Request
						}
						assistantRunMust(t, json.Unmarshal(op.Intent, &intent))
						key := wireRequest{intent.Suffix, string(intent.Request.Body)}
						wireMu.Lock()
						if physical[key] == 0 {
							t.Error("durable dispatch has no physical request with its exact path/query/body")
						} else {
							physical[key]--
						}
						wireMu.Unlock()
					}
				}
				if int(arrived.Load()) != dispatched {
					t.Errorf("%d physical requests have %d durable dispatch receipts", arrived.Load(), dispatched)
				}
				if interrupted.Load() > 0 && (uncertain == 0 || e.ctx.Err() == nil) {
					t.Error("physical transport interruption did not retain uncertainty and close execution authority")
				}
				if timeoutReplies.Load() > 0 && interrupted.Load() == 0 && uncertain != 0 {
					t.Error("complete HTTP timeout left an uncertain dispatch receipt")
				}
			})
			return call
		}
		critic, child := run.ModelName, run.ModelName
		if run.Models != nil {
			models, err := run.Models()
			if err != nil {
				return err
			}
			critic, child = models.Critic, models.Child
		}
		held.Model = bind(run.Wire, run.ModelName, critic, child, run.Model)
		if run.PolicyConnections != nil {
			held.PolicyConnections = make(map[string]adapter.PolicyConnection, len(run.PolicyConnections))
			for name, value := range run.PolicyConnections {
				value.Model = bind(value.Wire, value.ModelName, value.ModelName, value.ModelName, value.Model)
				held.PolicyConnections[name] = value
			}
		}
		return inner.Run(ctx, &held)
	})
}

func TestAssistantBindingCertificationAPI(t *testing.T) {
	server := assistantBindingCertificationServer(t)
	assistantCertificationPhysical.Store(0)
	previousTransport := relayTransport
	relayTransport = http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(func() { relayTransport = previousTransport })
	with := func(base certification.Adapter) certification.Adapter { return delegate.With(base, delegate.Options{}) }
	checks := 0
	report := func(t *testing.T, findings []certification.Finding, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for _, finding := range findings {
			t.Error(finding.String())
		}
		checks++
	}
	for _, endpoint := range certification.AllLegs {
		for _, traced := range []bool{false, true} {
			name := fmt.Sprintf("engine/%s/%s/traced=%t", endpoint.API, endpoint.AnswerAs, traced)
			t.Run(name, func(t *testing.T) {
				bounded, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				var result *certification.LegResult
				var err error
				if traced {
					result, err = certification.ScenarioLegTraced(bounded, server, endpoint, assistantBindingCertificationAPI(t, eino.New()))
				} else {
					result, err = certification.ScenarioLeg(bounded, server, endpoint, assistantBindingCertificationAPI(t, eino.New()))
				}
				if err != nil {
					t.Fatal(err)
				}
				report(t, result.Checks(), nil)
			})
			if t.Failed() {
				return
			}
		}
	}
	for _, check := range certification.LegChecks() {
		for _, endpoint := range check.Legs {
			t.Run(check.Name+"/"+endpoint.API+"/"+endpoint.AnswerAs, func(t *testing.T) {
				bounded, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				findings, err := check.Run(bounded, server, endpoint, assistantBindingCertificationAPI(t, eino.New()))
				report(t, findings, err)
			})
			if t.Failed() {
				return
			}
		}
	}
	for _, scenario := range certification.AllScenarios(with) {
		for _, shape := range []string{"stream", "whole"} {
			t.Run("scenario/"+scenario.Scenario+"/"+shape, func(t *testing.T) {
				bounded, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				findings, err := scenario.Run(bounded, server, shape, assistantBindingCertificationAPI(t, eino.New()))
				report(t, findings, err)
			})
			if t.Failed() {
				return
			}
		}
	}
	t.Logf("Desk actual relay binding: %d released legs passed, %d physical provider requests; production purpose factory and native binding are separate checks", checks, assistantCertificationPhysical.Load())
}
