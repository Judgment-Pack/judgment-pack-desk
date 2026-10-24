package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var modelSourceTestChoice = backgroundModelChoice{Connection: "test-connection", Revision: "test-revision", Model: "test-model"}

func modelBrokerFixture(t *testing.T) *modelSourceBroker {
	t.Helper()
	b, err := newModelSourceBroker()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.close)
	raw, _ := json.Marshal(b.config)
	t.Setenv(modelSourceEnvironment, string(raw))
	return b
}
func TestModelSourceConsumesAdmittedOperationExactlyOnce(t *testing.T) {
	b := modelBrokerFixture(t)
	var calls atomic.Int32
	const value = `{"fields":{"count":{"status":"found","value":0,"cites":[0]},"eligible":{"status":"found","value":false,"cites":[0]},"amount":{"status":"found","value":"1.00","cites":[0]}}}`
	request, cleanup, err := b.register(context.Background(), modelSourceTestChoice, []byte(`{"question":"exact frozen question"}`), func(context.Context) (json.RawMessage, error) { calls.Add(1); return json.RawMessage(value), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := json.Marshal(request)
	var result bytes.Buffer
	if err := RunModelSourceAdapter(bytes.NewReader(raw), &result); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result      json.RawMessage `json:"result"`
		Acquisition struct {
			Endpoint string            `json:"endpoint"`
			Adapter  map[string]string `json:"adapter"`
		} `json:"acquisition"`
	}
	if json.Unmarshal(result.Bytes(), &envelope) != nil || string(envelope.Result) != value || envelope.Acquisition.Endpoint != modelSourceEndpoint(modelSourceTestChoice) || envelope.Acquisition.Adapter["name"] != "jpack-desk/model-answer" {
		t.Fatalf("lost response %s", result.Bytes())
	}
	if strings.Contains(result.String(), b.config.Token) {
		t.Fatal("broker credential leaked")
	}
	result.Reset()
	if err := RunModelSourceAdapter(bytes.NewReader(raw), &result); err == nil || calls.Load() != 1 || result.Len() != 0 {
		t.Fatal("consumed model operation replayed")
	}
}
func TestModelSourceRefusesBrowserWrongDigestAndConcurrentReplay(t *testing.T) {
	b := modelBrokerFixture(t)
	var calls atomic.Int32
	request, cleanup, err := b.register(context.Background(), modelSourceTestChoice, []byte(`{"question":"frozen"}`), func(context.Context) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{"ok":true}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := json.Marshal(request)
	for _, headers := range []map[string]string{{}, {"Authorization": "Bearer wrong"}, {"Authorization": "Bearer " + b.config.Token, "Origin": "http://localhost"}, {"Authorization": "Bearer " + b.config.Token, "Sec-Fetch-Site": "same-origin"}} {
		r := httptest.NewRequest("POST", "/answer", bytes.NewReader(raw))
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		b.serve(w, r)
		if w.Code != 401 || calls.Load() != 0 {
			t.Fatal("unadmitted request accepted")
		}
	}
	changed := request
	changed.Arguments.Digest = sha256Digest([]byte(`{}`))
	bad, _ := json.Marshal(changed)
	var output bytes.Buffer
	if RunModelSourceAdapter(bytes.NewReader(bad), &output) == nil || calls.Load() != 0 {
		t.Fatal("changed operation dispatched")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			if RunModelSourceAdapter(bytes.NewReader(raw), &out) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("duplicate dispatch: success %d calls %d", successes.Load(), calls.Load())
	}
}
func TestModelSourceFailureAndCancellationAreNeverReplayed(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancelled"}[cancelled], func(t *testing.T) {
			b := modelBrokerFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			request, cleanup, err := b.register(ctx, modelSourceTestChoice, []byte(`{}`), func(context.Context) (json.RawMessage, error) {
				calls.Add(1)
				return nil, errors.New("PRIVATE_PROVIDER_PROSE")
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if cancelled {
				cancel()
			}
			raw, _ := json.Marshal(request)
			for i := 0; i < 2; i++ {
				var out bytes.Buffer
				err := RunModelSourceAdapter(bytes.NewReader(raw), &out)
				if err == nil || strings.Contains(err.Error(), "PRIVATE_PROVIDER_PROSE") || out.Len() != 0 {
					t.Fatal("uncertain acquisition replayed or unsafe diagnostics")
				}
			}
			want := int32(1)
			if cancelled {
				want = 0
			}
			if calls.Load() != want {
				t.Fatal("wrong operation count", calls.Load())
			}
		})
	}
}
func TestModelSourceDisconnectCancelsAdmittedOperation(t *testing.T) {
	b := modelBrokerFixture(t)
	entered, finished := make(chan struct{}), make(chan struct{})
	request, cleanup, err := b.register(context.Background(), modelSourceTestChoice, []byte(`{}`), func(ctx context.Context) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		close(finished)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := json.Marshal(request)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", b.config.URL+"/answer", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+b.config.Token)
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(r)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not enter")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnected operation continued")
	}
	<-done
}
func TestModelSourceRealGatewayReceipt(t *testing.T) {
	bundle := os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if bundle == "" {
		t.Skip("requires pinned Gateway")
	}
	s, ts := startDesk(t, Config{LocalAccess: true, ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, LocalGatewayBundle: bundle})
	defer ts.Close()
	defer s.Close()
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	// This callback stands in for an already journaled shared model operation;
	// Gateway and its adapter are real. No live model/provider is contacted.
	request, cleanup, err := s.localGateway.modelSource.register(context.Background(), modelSourceTestChoice, []byte(`{"question":"Retain false without coercion"}`), func(context.Context) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{"fields":{"eligible":{"status":"found","value":false,"cites":[0]}}}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	body, _ := json.Marshal(map[string]any{"session": "model-source-integration", "source": modelSourceName, "arguments": request})
	response, err := http.Post(pin.URL+"/acquire", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("acquire %d: %s %v", response.StatusCode, raw, err)
	}
	var envelope struct {
		Result  json.RawMessage `json:"result"`
		Receipt struct {
			Source      string `json:"source"`
			Acquisition struct {
				Endpoint string            `json:"endpoint"`
				Adapter  map[string]string `json:"adapter"`
			} `json:"acquisition"`
		} `json:"receipt"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Receipt.Source != modelSourceName || envelope.Receipt.Acquisition.Endpoint != modelSourceEndpoint(modelSourceTestChoice) || envelope.Receipt.Acquisition.Adapter["name"] != "jpack-desk/model-answer" || !bytes.Contains(envelope.Result, []byte(`"value":false`)) || calls.Load() != 1 {
		t.Fatalf("wrong acquisition %s", raw)
	}
	// A different Gateway request cannot reuse an admitted operation.
	response2, err := http.Post(pin.URL+"/acquire", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response2.Body.Close()
	if calls.Load() != 1 {
		t.Fatal("Gateway replay invoked model twice")
	}
}

func TestModelSourceBoundPreservesSpecialCharacterBytes(t *testing.T) {
	b := modelBrokerFixture(t)
	// Include characters json.Encoder escapes even inside a RawMessage.
	raw := append([]byte(`"`), []byte(strings.Repeat("<>&\u2028", (backgroundModelResponseBytes-2)/6))...)
	raw = append(raw, '"')
	request, cleanup, err := b.register(context.Background(), modelSourceTestChoice, []byte(`{}`), func(context.Context) (json.RawMessage, error) { return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	requestRaw, _ := json.Marshal(request)
	var out bytes.Buffer
	if err := RunModelSourceAdapter(bytes.NewReader(requestRaw), &out); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(out.Bytes(), &reply) != nil || !bytes.Equal(reply.Result, raw) {
		t.Fatal("accepted result was expanded or changed")
	}
}

func TestModelSourceDeadlineAndGatewayBudget(t *testing.T) {
	b := modelBrokerFixture(t)
	request, cleanup, err := b.register(context.Background(), modelSourceTestChoice, []byte(`{}`), func(ctx context.Context) (json.RawMessage, error) {
		deadline, ok := ctx.Deadline()
		left := time.Until(deadline)
		if !ok || left > backgroundModelTimeout || left < backgroundModelTimeout-time.Second {
			t.Errorf("wrong deadline %v %v", deadline, ok)
		}
		return json.RawMessage(`{}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ := json.Marshal(request)
	var out bytes.Buffer
	if err := RunModelSourceAdapter(bytes.NewReader(raw), &out); err != nil {
		t.Fatal(err)
	}
	args := modelSourceGatewayArgs("desk")
	timeout := 0
	for i, arg := range args {
		if arg == "--source-timeout" {
			timeout, _ = strconv.Atoi(strings.TrimPrefix(args[i+1], modelSourceName+"="))
		}
	}
	if time.Duration(timeout)*time.Second <= backgroundModelTimeout+privateDataLockWait {
		t.Fatal("Gateway would cut off the admitted model operation", args)
	}
}

func TestModelSourceSharedExecutorAndRunnerPreparation(t *testing.T) {
	bundle, runner, runtime := os.Getenv("JPS_TEST_GATEWAY_BUNDLE"), os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bundle == "" || runner == "" || runtime == "" {
		t.Skip("requires pinned Gateway, Runner and Runtime")
	}
	var calls atomic.Int32
	answer := `{"fields":{"eligible":{"status":"found","value":false,"cites":[0]}}}`
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": answer}}}})
	}))
	defer provider.Close()
	s, ts := startDesk(t, Config{LocalAccess: true, ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, LocalGatewayBundle: bundle, RunnerBin: runner, JpackBin: runtime})
	defer ts.Close()
	defer s.Close()
	connection := aiTestConnection(testAIAlpha, "Portable research", provider.URL)
	connection.Assistant.Endpoint.Models = []string{"shared-model", "other-model"}
	registry := saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{connection}})
	c := registry.Connections[0]
	code, raw := aiTestRequest(t, ts, "PUT", "/api/assistant/key", map[string]string{"key": "model-source-fixture-key"}, map[string]string{aiConnectionHeader: c.ID, aiRevisionHeader: c.Revision})
	if code != 200 {
		t.Fatalf("key: %d %s", code, raw)
	}
	choice := backgroundModelChoice{Connection: c.ID, Revision: c.Revision, Model: "shared-model"}
	profileRaw, err := s.registerModelSourceProfile(context.Background(), choice)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
		Digest string `json:"digest"`
	}
	if json.Unmarshal(profileRaw, &profile) != nil || profile.Profile.ID == "" || profile.Digest == "" {
		t.Fatalf("invalid registered profile: %s", profileRaw)
	}
	if calls.Load() != 0 {
		t.Fatal("profile registration called a model")
	}
	again, err := s.registerModelSourceProfile(context.Background(), choice)
	if err != nil || !bytes.Equal(profileRaw, again) {
		t.Fatalf("registration was not idempotent: %s %s %v", profileRaw, again, err)
	}
	task := backgroundFixture(choice)
	fixtureCtx := backgroundFixtureContext(t, &task)
	request, cleanup, err := s.localGateway.modelSource.register(fixtureCtx, choice, []byte(task.Prompt), func(ctx context.Context) (json.RawMessage, error) { return s.runBackgroundModel(ctx, task, nil) })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"session": "portable-model-preparation", "source": modelSourceName, "arguments": request})
	response, err := http.Post(pin.URL+"/acquire", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("acquisition %d %s %v", response.StatusCode, raw, err)
	}
	var acquired struct {
		Result  json.RawMessage `json:"result"`
		Receipt json.RawMessage `json:"receipt"`
		Salts   json.RawMessage `json:"salts"`
	}
	if json.Unmarshal(raw, &acquired) != nil {
		t.Fatal("invalid retained acquisition")
	}
	var generated struct {
		Fields json.RawMessage `json:"fields"`
	}
	if json.Unmarshal(acquired.Result, &generated) != nil {
		t.Fatal("invalid retained model answer")
	}
	retained := map[string]any{"profile": map[string]string{"id": profile.Profile.ID, "digest": profile.Digest}, "request": request, "result": acquired.Result, "receipt": acquired.Receipt, "salts": acquired.Salts}
	record := map[string]any{"fields": generated.Fields, "responses": []any{retained}}
	mapping := map[string]any{"version": 3, "sources": []any{map[string]any{"name": "answer", "kind": "research", "profile": profile.Profile.ID, "profileDigest": profile.Digest, "maxAge": 3600, "question": "Retain eligibility exactly", "constraints": map[string]any{}, "model": choice.Model, "capabilities": []string{"answer"}, "webProfiles": map[string]any{}, "evidence": []string{}, "fields": []any{map[string]any{"name": "eligible", "description": "Whether this record is eligible.", "type": "boolean", "required": true}}, "read": map[string]any{"copy": map[string]any{"facts": []any{map[string]string{"source": "/fields/eligible/value", "target": "/eligible"}}, "evidence": []any{}}}}}}
	input := map[string]any{"source": map[string]any{"mapping": mapping, "case": map[string]any{}, "sources": map[string]any{"answer": map[string]any{"response": map[string]any{"result": record}}}}}
	if _, err := s.inputRunner(context.Background(), "inputs/preview", input, ""); err == nil {
		t.Fatal("generated facts were admitted without a declared policy")
	}
	mapping["admits"] = map[string]any{"facts": map[string]any{"/eligible": []string{"generated"}}}
	prepared, err := s.inputRunner(context.Background(), "inputs/preview", input, "")
	if err != nil {
		t.Fatalf("Runner refused actual model receipt: %v", err)
	}
	if !bytes.Contains(prepared, []byte(`"generatedInfluence":true`)) || !bytes.Contains(prepared, []byte(`"class":"generated"`)) || calls.Load() != 1 {
		t.Fatalf("lost lineage/call count: %s %d", prepared, calls.Load())
	}
	// A receipt from the admitted model cannot be relabeled as another model,
	// even when both profiles share this installation's signing key and adapter.
	otherChoice := choice
	otherChoice.Model = "other-model"
	otherRaw, err := s.registerModelSourceProfile(context.Background(), otherChoice)
	if err != nil {
		t.Fatal(err)
	}
	var otherProfile struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
		Digest string `json:"digest"`
	}
	if json.Unmarshal(otherRaw, &otherProfile) != nil {
		t.Fatal("invalid other profile")
	}
	source := mapping["sources"].([]any)[0].(map[string]any)
	retained["profile"] = map[string]string{"id": otherProfile.Profile.ID, "digest": otherProfile.Digest}
	source["profile"], source["profileDigest"], source["model"] = otherProfile.Profile.ID, otherProfile.Digest, otherChoice.Model
	if _, err := s.inputRunner(context.Background(), "inputs/preview", input, ""); err == nil {
		t.Fatal("receipt relabeled as a different model passed verification")
	}
	retained["profile"] = map[string]string{"id": profile.Profile.ID, "digest": profile.Digest}
	source["profile"], source["profileDigest"], source["model"] = profile.Profile.ID, profile.Digest, choice.Model
	// Restoring only the profile succeeds, isolating the refusal to model identity.
	if _, err := s.inputRunner(context.Background(), "inputs/preview", input, ""); err != nil {
		t.Fatal(err)
	}
	// Tampering with the retained MODEL response must fail its original receipt.
	// R.fields on its own remains the caller's assertion, as Spec §4.3 states.
	retained["result"] = json.RawMessage(strings.Replace(answer, `"value":false`, `"value":true`, 1))
	if _, err := s.inputRunner(context.Background(), "inputs/preview", input, ""); err == nil {
		t.Fatal("tampered model response passed receipt verification")
	}
	if calls.Load() != 1 {
		t.Fatal("offline preparation invoked a model")
	}
}

func TestModelSourceEndpointPinsEveryChoiceMember(t *testing.T) {
	original := modelSourceTestChoice
	for _, field := range []string{"connection", "revision", "model", "effort"} {
		changed := original
		switch field {
		case "connection":
			changed.Connection += "-new"
		case "revision":
			changed.Revision += "-new"
		case "model":
			changed.Model += "-new"
		case "effort":
			changed.Effort = "high"
		}
		if modelSourceEndpoint(original) == modelSourceEndpoint(changed) {
			t.Fatal("unbound choice", field)
		}
	}
}
