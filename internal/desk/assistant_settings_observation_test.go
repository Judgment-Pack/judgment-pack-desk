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
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type settingsObservationFixture struct {
	s          *Server
	token      string
	calls      atomic.Int32
	body       []byte
	headers    http.Header
	connection aiConnection
	legacy     bool
}

func newSettingsObservationFixture(t *testing.T, family string, status int, reply string, legacy, disabled bool) *settingsObservationFixture {
	t.Helper()
	f := &settingsObservationFixture{legacy: legacy}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.body, _ = io.ReadAll(r.Body)
		f.headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(upstream.Close)
	f.s, f.token = policyTestServer(t)
	f.connection = aiTestConnection(testAIAlpha, "Settings fixture", upstream.URL)
	f.connection.Assistant.Endpoint.Kind = family
	f.connection.Enabled = !disabled
	if legacy {
		f.connection.ID = "legacy-api"
		raw, _ := json.Marshal(map[string]any{"deskConfigVersion": 1, "assistant": f.connection.Assistant})
		assistantRunMust(t, os.WriteFile(filepath.Join(f.s.configDir, "desk.json"), raw, 0600))
	} else {
		defaultConnection := f.connection.ID
		if disabled {
			defaultConnection = ""
		}
		raw, err := encodeAIRegistry(aiRegistry{Version: 1, DefaultConnection: defaultConnection, Connections: []aiConnection{f.connection}})
		assistantRunMust(t, err)
		assistantRunMust(t, f.s.assistant.writeConfigNamed(aiConnectionsFile, raw, nil))
	}
	origin, _ := endpointOrigin(upstream.URL)
	assistantRunMust(t, f.s.assistant.storeKeyNamed(connectionKeyName(&f.connection), storedKey{present: true, key: "settings-private-key", origin: origin, kind: family}))
	return f
}
func (f *settingsObservationFixture) call(method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	if wire, ok := body.(json.RawMessage); ok {
		raw = wire
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+f.token)
	if !f.legacy {
		r.Header.Set(aiConnectionHeader, f.connection.ID)
		registry, _ := f.s.readAIRegistry()
		if len(registry.Connections) > 0 {
			r.Header.Set(aiRevisionHeader, registry.Connections[0].Revision)
		}
	}
	w := httptest.NewRecorder()
	f.s.ServeHTTP(w, r)
	return w
}
func (f *settingsObservationFixture) reference(t *testing.T, id, kind string) assistantRunStart {
	t.Helper()
	body := map[string]string{"requestId": id, "kind": kind}
	if kind == "capability" {
		body["model"] = "shared-model"
	}
	w := f.call("POST", "/api/assistant-observations/reference", body)
	if w.Code != 200 {
		t.Fatalf("reference %d: %s", w.Code, w.Body.String())
	}
	var ref assistantObservationReference
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &ref))
	if ref.Start == nil {
		t.Fatal("missing reference")
	}
	return *ref.Start
}
func (f *settingsObservationFixture) run(t *testing.T, start assistantRunStart) assistantRunRecord {
	t.Helper()
	view := assistantProtocolView(t, f.call("POST", "/api/assistant-runs", start), 201)
	h := f.s.assistantRunService()
	h.mu.Lock()
	e := h.active[view.ID]
	h.mu.Unlock()
	if e != nil {
		select {
		case <-e.done:
		case <-time.After(15 * time.Second):
			t.Fatal("Settings attempt did not finish")
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	run, err := h.read(req, view.ID)
	assistantRunMust(t, err)
	if !assistantRunTerminal(run.State) {
		t.Fatalf("nonterminal %s", run.State)
	}
	return run
}
func (f *settingsObservationFixture) result(t *testing.T, run assistantRunRecord) assistantSettingsObservationResult {
	t.Helper()
	w := f.call("GET", "/api/assistant-runs/"+run.ID+"/settings-observation", nil)
	if w.Code != 200 {
		t.Fatalf("result %d %s; state=%s checkpoint=%s outcome=%+v", w.Code, w.Body.String(), run.State, run.Checkpoint.Status, run.Observation.Outcome)
	}
	var p assistantSettingsObservationResult
	assistantRunMust(t, json.Unmarshal(w.Body.Bytes(), &p))
	return p
}
func TestAssistantSettingsObservationFixedRun(t *testing.T) {
	for _, kind := range []string{"capability", "ping"} {
		t.Run(kind, func(t *testing.T) {
			family, text, ceiling := "openai-compatible", assistantCapabilityInstruction, 128
			if kind == "ping" {
				family, text, ceiling = "anthropic", "ping", 1
			}
			source := assistantRunSource{Start: assistantRunStart{Purpose: assistantSettingsObservationPurpose, Observation: &assistantObservationSelector{kind, "fixture"}}, Binding: assistantRunBinding{ConnectionID: "legacy-api", ConnectionRevision: strings.Repeat("a", 64), Model: "fixture", Phase: "work"}, Text: text, Connection: aiConnection{Assistant: AssistantSlotView{Endpoint: &AssistantEndpointView{Kind: family}}}}
			run, session, options, err := assistantSettingsObservationRun(source)
			assistantRunMust(t, err)
			if run.RequestOutputCeiling != ceiling || run.Agent != nil || run.Children != nil || len(run.Tools) != 0 || len(run.HostTools) != 0 || run.Context.RequestCap == nil || *run.Context.RequestCap != 1 || run.Context.Memory == nil || *run.Context.Memory || run.Context.ArtifactProposals == nil || *run.Context.ArtifactProposals || run.Context.Configuration != nil || session.Model.Thinking != "off" || session.Critic != "off" {
				t.Fatal("fixed observation acquired unrelated context or execution")
			}
			if kind == "capability" && (len(options.Tools) != 1 || string(options.Tools[0].InputSchema) != assistantObservationSchema || options.Tools[0].Taxonomy.Operation != "transform" || options.Tools[0].Taxonomy.Effect != "pure") {
				t.Fatal("schema-only offer changed")
			}
			if kind == "ping" && len(options.Tools) != 0 {
				t.Fatal("ping offered a tool")
			}
			cp, err := (&assistantSettingsObservationExecutor{}).Prepare(t.Context(), source)
			assistantRunMust(t, err)
			if cp.Unit != "model-requests" || cp.Used != 0 || cp.Limit != 1 || cp.Durable != nil {
				t.Fatal("Prepare created durable work")
			}
		})
	}
}
func TestAssistantSettingsObservationCapabilityResponses(t *testing.T) {
	rows := []struct {
		name, reply                    string
		status                         int
		native, parallel, complete, ok bool
		reason, problem                string
	}{
		{"zero", `{"choices":[{"message":{"content":"provider-private-text"},"finish_reason":"stop"}]}`, 200, false, false, true, true, "", ""},
		{"one", `{"choices":[{"message":{"tool_calls":[{"id":"a","type":"function","function":{"name":"probe_echo","arguments":"{\"value\":1}"}}]},"finish_reason":"tool_calls"}]}`, 200, true, false, true, true, "", ""},
		{"two-reasoning", `{"choices":[{"message":{"reasoning_content":"","reasoning":"private-reasoning","tool_calls":[{"id":"a","type":"function","function":{"name":"probe_echo","arguments":"{\"value\":1}"}},{"id":"b","type":"function","function":{"name":"probe_echo","arguments":"{\"value\":2}"}}]},"finish_reason":"tool_calls"}]}`, 200, true, true, true, true, "reasoning_content", ""},
		{"tools400", `{"error":{"message":"private-provider-error"}}`, 400, false, false, false, true, "", ""},
		{"tools422", `{"error":{"message":"private-provider-error"}}`, 422, false, false, false, true, "", ""},
		{"unauthorized", `{"error":{"message":"private-provider-error"}}`, 401, false, false, false, false, "", "refused"},
		{"length", `{"choices":[{"message":{"content":"partial-private-text"},"finish_reason":"length"}]}`, 200, false, false, false, false, "", "unreadable"},
		{"unoffered", `{"choices":[{"message":{"tool_calls":[{"id":"a","type":"function","function":{"name":"foreign","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, 200, false, false, false, false, "", "unreadable"},
		{"malformed", `{"choices":[`, 200, false, false, false, false, "", "unreadable"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := newSettingsObservationFixture(t, "openai-compatible", row.status, row.reply, false, false)
			start := f.reference(t, "capability-"+row.name, "capability")
			if f.calls.Load() != 0 {
				t.Fatal("reference called provider")
			}
			run := f.run(t, start)
			p := f.result(t, run)
			if f.calls.Load() != 1 {
				t.Fatalf("physical requests=%d", f.calls.Load())
			}
			if p.ObserveSucceeded != row.complete || p.Capability == nil || p.Capability.OK != row.ok || p.Capability.Problem != row.problem {
				t.Fatalf("unexpected outcome %+v, canonical=%s", p, run.Checkpoint.Status)
			}
			if row.ok {
				facts := p.Capability.Findings.Facts
				if facts.NativeToolCalls != row.native || facts.ParallelToolCalls != row.parallel || facts.ReasoningField != row.reason {
					t.Fatalf("facts %+v", facts)
				}
			}
			if err := assistantSettingsWire(assistantRunSource{Start: start, Binding: run.Binding, Connection: f.connection}, "chat/completions", relayObservationRequest(httptest.NewRequest("POST", "/", nil), f.body)); err != nil {
				t.Fatal("actual encoded fixed body", err, string(f.body))
			}
			if bytes.Contains(f.body, []byte(`"parallel_tool_calls":false`)) {
				t.Fatal("measurement inherited a parallel refusal")
			}
			before := f.calls.Load()
			duplicate := f.call("POST", "/api/assistant-runs", start)
			if duplicate.Code != 200 {
				t.Fatalf("duplicate %d %s", duplicate.Code, duplicate.Body.String())
			}
			_ = f.result(t, run)
			lookup := f.call("GET", "/api/assistant-observations/requests/"+start.RequestID, nil)
			if lookup.Code != 200 {
				t.Fatal("lookup", lookup.Code)
			}
			view := f.call("GET", "/api/assistant-runs/"+run.ID, nil)
			events := f.call("GET", "/api/assistant-runs/"+run.ID+"/events", nil)
			for _, public := range [][]byte{view.Body.Bytes(), events.Body.Bytes(), lookup.Body.Bytes()} {
				for _, secret := range []string{"private-", "settings-private-key", "probe_echo", "chat/completions", "reasoning_content", "max_tokens"} {
					if bytes.Contains(public, []byte(secret)) {
						t.Fatalf("public Settings leaked %q: %s", secret, public)
					}
				}
			}
			if f.calls.Load() != before {
				t.Fatal("read or duplicate replayed")
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", "Bearer "+f.token)
			persisted, err := f.s.assistantRunService().read(req, run.ID)
			assistantRunMust(t, err)
			if persisted.Observation.Outcome == nil || validateAssistantSettingsOutcome(persisted) != nil {
				t.Fatal("retained outcome unavailable")
			}
		})
	}
}
func TestAssistantSettingsObservationPingResponses(t *testing.T) {
	for _, row := range []struct {
		name, reason        string
		status              int
		reachable, complete bool
	}{{"complete", "end_turn", 200, true, true}, {"length", "max_tokens", 200, true, false}, {"unauthorized", "", 401, false, false}} {
		t.Run(row.name, func(t *testing.T) {
			reply := `{"id":"msg","type":"message","role":"assistant","model":"shared-model","content":[{"type":"text","text":"provider-private-text"}],"stop_reason":"` + row.reason + `","usage":{"input_tokens":1,"output_tokens":1}}`
			if row.status != 200 {
				reply = `{"error":{"message":"private-provider-error"}}`
			}
			f := newSettingsObservationFixture(t, "anthropic", row.status, reply, false, true)
			// Ping preserves the former Settings path even without a usable chat profile.
			assistantRunMust(t, os.WriteFile(filepath.Join(f.s.cfg.ProjectDir, assistantProfilePath), []byte(`{"broken":true}`), 0600))
			start := f.reference(t, "ping-"+row.name, "ping")
			run := f.run(t, start)
			p := f.result(t, run)
			if f.calls.Load() != 1 || p.Ping == nil || p.Ping.Reachable != row.reachable || p.Ping.Status != row.status || p.ObserveSucceeded != row.complete || p.Ping.ObservationComplete == nil || *p.Ping.ObservationComplete != row.complete {
				t.Fatalf("ping outcome %+v count=%d", p, f.calls.Load())
			}
			var body map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(f.body, &body))
			if string(body["max_tokens"]) != "1" || len(body["tools"]) != 0 || f.headers.Get("anthropic-version") != "2023-06-01" || f.headers.Get("x-api-key") != "settings-private-key" {
				t.Fatal("ping wire, version or credential changed", string(f.body))
			}
			if row.reachable && !row.complete && p.Failure == nil {
				t.Fatal("length refusal lost")
			}
			// Matching public text/headers never gives the ordinary relay its exception.
			ordinary := f.call("POST", relayPrefix+"v1/messages", json.RawMessage(`{"model":"shared-model","max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`))
			if ordinary.Code < 400 || f.calls.Load() != 1 {
				t.Fatal("ordinary relay gained ping exception")
			}
		})
	}
}
func TestAssistantSettingsObservationLegacy(t *testing.T) {
	for _, kind := range []string{"capability", "ping"} {
		t.Run(kind, func(t *testing.T) {
			family, reply := "openai-compatible", `{"choices":[{"message":{"content":"P"},"finish_reason":"stop"}]}`
			if kind == "ping" {
				family, reply = "anthropic", `{"id":"msg","type":"message","role":"assistant","model":"shared-model","content":[{"type":"text","text":"P"}],"stop_reason":"end_turn"}`
			}
			f := newSettingsObservationFixture(t, family, 200, reply, true, false)
			start := f.reference(t, "legacy-"+kind, kind)
			if start.OwnerID != "legacy-api" {
				t.Fatal("lost actual legacy path")
			}
			run := f.run(t, start)
			p := f.result(t, run)
			if !p.ObserveSucceeded || f.calls.Load() != 1 {
				t.Fatalf("legacy observation %+v", p)
			}
		})
	}
}
func TestAssistantSettingsObservationClosedAdmission(t *testing.T) {
	f := newSettingsObservationFixture(t, "openai-compatible", 200, `{}`, false, false)
	for _, body := range []string{`{}`, `{"requestId":"x","kind":"foreign"}`, `{"requestId":"x","kind":"capability","model":"shared-model","prompt":"foreign"}`, `{"requestId":"x","kind":"capability","model":"foreign"}`, `{"requestId":"x","kind":"capability","model":"shared-model","ceiling":1}`} {
		if w := f.call("POST", "/api/assistant-observations/reference", json.RawMessage(body)); w.Code < 400 {
			t.Fatal("accepted invalid reference", body)
		}
	}
	start := f.reference(t, "closed", "capability")
	raw, _ := json.Marshal(start)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, selector := range []string{`null`, `{}`, `{"kind":"capability","model":"shared-model","ceiling":1}`, `{"Kind":"capability","model":"shared-model"}`} {
		fields["observation"] = json.RawMessage(selector)
		body, _ := json.Marshal(fields)
		if w := f.call("POST", "/api/assistant-runs", json.RawMessage(body)); w.Code < 400 {
			t.Fatal("accepted foreign selector", selector)
		}
	}
	for _, owner := range []string{"", "malformed", "ai-ffffffffffffffffffffffff", "legacy-api", "legacy-codex"} {
		wrongOwner := start
		wrongOwner.OwnerID = owner
		if w := f.call("POST", "/api/assistant-runs", wrongOwner); w.Code < 400 {
			t.Fatal("accepted foreign owner", owner)
		}
	}
	foreign := start
	foreign.Purpose = "chat"
	if w := f.call("POST", "/api/assistant-runs", foreign); w.Code < 400 {
		t.Fatal("selector crossed purpose")
	}
	stale := start
	stale.Observation = &assistantObservationSelector{"capability", "foreign"}
	if w := f.call("POST", "/api/assistant-runs", stale); w.Code < 400 {
		t.Fatal("selected foreign model")
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid admission dispatched")
	}
}
func TestAssistantSettingsObservationOutcomeCustody(t *testing.T) {
	f := newSettingsObservationFixture(t, "openai-compatible", 200, `{"choices":[{"message":{"content":"P"},"finish_reason":"stop"}]}`, false, false)
	run := f.run(t, f.reference(t, "custody", "capability"))
	for _, mutate := range []func(*assistantRunRecord){func(r *assistantRunRecord) { r.Observation.Outcome.OperationID = "foreign" }, func(r *assistantRunRecord) { r.Observation.Outcome.CanonicalHead.Revision = "stale" }, func(r *assistantRunRecord) {
		r.Observation.Outcome.Public.Capability.Findings.Facts.NativeToolCalls = true
	}, func(r *assistantRunRecord) { r.Operations[0].State = "refused" }, func(r *assistantRunRecord) { r.Observation.Outcome.Public.ObserveSucceeded = false }} {
		raw, _ := json.Marshal(run)
		var bad assistantRunRecord
		assistantRunMust(t, json.Unmarshal(raw, &bad))
		mutate(&bad)
		if validateAssistantSettingsOutcome(bad) == nil {
			t.Fatal("accepted corrupted outcome")
		}
	}
	raw, _ := json.Marshal(run.Observation)
	var held map[string]json.RawMessage
	_ = json.Unmarshal(raw, &held)
	var outcome map[string]json.RawMessage
	_ = json.Unmarshal(held["outcome"], &outcome)
	outcome["foreign"] = json.RawMessage(`true`)
	held["outcome"], _ = json.Marshal(outcome)
	bad, _ := json.Marshal(held)
	if readAssistantObservationCustody(bad) == nil {
		t.Fatal("accepted unknown outcome member")
	}
	delete(outcome, "foreign")
	outcome["operationId"] = json.RawMessage(`null`)
	held["outcome"], _ = json.Marshal(outcome)
	bad, _ = json.Marshal(held)
	if readAssistantObservationCustody(bad) == nil {
		t.Fatal("accepted malformed outcome member")
	}
	if !reflect.DeepEqual(f.result(t, run), f.result(t, run)) {
		t.Fatal("result changed across read")
	}
}

// A provider never replies: no completed receipt, findings, or retry is possible.
func TestAssistantSettingsObservationTransportLoss(t *testing.T) {
	f := newSettingsObservationFixture(t, "anthropic", 200, `{}`, false, false)
	previous := relayTransport
	relayTransport = settingsObservationBrokenTransport{}
	t.Cleanup(func() { relayTransport = previous })
	run := f.run(t, f.reference(t, "transport-loss", "ping"))
	p := f.result(t, run)
	if run.Observation.Outcome != nil || p.ObserveSucceeded || p.Ping == nil || p.Ping.Reachable || p.Ping.Status != 0 || p.Ping.Diagnostic == "" || f.calls.Load() != 0 {
		t.Fatalf("uncertainty became evidence %+v", p)
	}
}

type settingsObservationBrokenTransport struct{}

func (settingsObservationBrokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

// A valid restrictive profile excludes the connection independently of Enabled.
func TestAssistantSettingsObservationProfileExcludedPing(t *testing.T) {
	f := newSettingsObservationFixture(t, "anthropic", 200, `{"id":"msg","type":"message","role":"assistant","model":"shared-model","content":[{"type":"text","text":"P"}],"stop_reason":"end_turn"}`, false, false)
	assistantRunMust(t, os.WriteFile(filepath.Join(f.s.cfg.ProjectDir, assistantProfilePath), []byte(`{"profileVersion":2,"inherit":false,"connections":[],"defaultConnection":null}`), 0600))
	profile, err := f.s.readAssistantProfile()
	assistantRunMust(t, err)
	if profile.Version != 2 || profile.Inherit == nil || *profile.Inherit || contains(profile.Connections, f.connection.ID) {
		t.Fatal("fixture did not exclude saved connection")
	}
	run := f.run(t, f.reference(t, "profile-excluded-ping", "ping"))
	if p := f.result(t, run); !p.ObserveSucceeded || p.Ping == nil || !p.Ping.Reachable || f.calls.Load() != 1 {
		t.Fatalf("excluded ping %+v calls=%d", p, f.calls.Load())
	}
	ordinary := f.call("POST", relayPrefix+"v1/messages", json.RawMessage(`{"model":"shared-model","max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`))
	if ordinary.Code < 400 || f.calls.Load() != 1 {
		t.Fatal("ordinary relay gained excluded-profile exception")
	}
}

func TestAssistantSettingsObservationReceiptRefusals(t *testing.T) {
	f := newSettingsObservationFixture(t, "openai-compatible", 200, `{"choices":[{"message":{"content":"P"},"finish_reason":"stop"}]}`, false, false)
	run := f.run(t, f.reference(t, "receipt-refusals", "capability"))
	for _, name := range []string{"duplicate", "foreign-model", "foreign-kind", "refused", "uncertain", "truncated-result", "oversized-result", "missing-latency", "foreign-suffix", "foreign-wire-model", "foreign-ceiling"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(run)
			var bad assistantRunRecord
			assistantRunMust(t, json.Unmarshal(raw, &bad))
			op := &bad.Operations[0]
			switch name {
			case "duplicate":
				bad.Operations = append(bad.Operations, *op)
			case "foreign-model":
				op.Name = "foreign"
			case "foreign-kind":
				op.Kind = "runtime"
			case "refused":
				op.State = "refused"
			case "uncertain":
				op.State = "uncertain"
			case "truncated-result":
				op.Result = json.RawMessage(`{"status":200`)
			case "oversized-result":
				op.Result = bytes.Repeat([]byte(" "), assistantRunRecordLimit+1)
			case "missing-latency":
				var receipt assistantSettingsPhysicalReceipt
				assistantRunMust(t, json.Unmarshal(op.Result, &receipt))
				receipt.LatencyMs = nil
				op.Result, _ = json.Marshal(receipt)
			default:
				var intent assistantSettingsPhysicalIntent
				assistantRunMust(t, json.Unmarshal(op.Intent, &intent))
				if name == "foreign-suffix" {
					intent.Suffix = "v1/messages"
				} else {
					var body map[string]json.RawMessage
					assistantRunMust(t, json.Unmarshal(intent.Request.Body, &body))
					if name == "foreign-wire-model" {
						body["model"] = json.RawMessage(`"foreign"`)
					} else {
						body["max_tokens"] = json.RawMessage(`1`)
					}
					intent.Request.Body, _ = json.Marshal(body)
				}
				op.Intent, _ = json.Marshal(intent)
			}
			if _, _, err := assistantSettingsReceipt(bad); err == nil {
				t.Fatal("foreign or incomplete physical evidence accepted")
			}
			if validateAssistantSettingsOutcome(bad) == nil {
				t.Fatal("outcome accepted without exact receipt")
			}
		})
	}
	if f.calls.Load() != 1 {
		t.Fatal("receipt validation replayed")
	}
}

func TestAssistantSettingsObservationLostOutcomeAndRestart(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "retained"
		if lost {
			name = "lost-write"
		}
		t.Run(name, func(t *testing.T) {
			f := newSettingsObservationFixture(t, "openai-compatible", 200, `{"choices":[{"message":{"content":"P"},"finish_reason":"stop"}]}`, false, false)
			start := f.reference(t, "restart-"+name, "capability")
			run := f.run(t, start)
			expected := f.result(t, run)
			canonicalBefore, _ := json.Marshal(run.Canonical)
			checkpointBefore, _ := json.Marshal(run.Checkpoint)
			store := f.s.assistantRuns()
			if lost {
				// Model and canonical completion are already retained. Exercise a failed
				// normalized-outcome write with no dispatch authority, then keep it absent.
				lease, err := store.Acquire(t.Context())
				assistantRunMust(t, err)
				writer, err := lease.Writer(t.Context(), run.Binding, run.ID)
				assistantRunMust(t, err)
				assistantRunMust(t, writer.change(t.Context(), true, func(r *assistantRunRecord) (bool, error) { r.Observation.Outcome = nil; return true, nil }))
				sentinel := errors.New("normalized Settings outcome write unavailable")
				injected := false
				testHookPrivateRead = func(name string) error {
					if name == store.name {
						injected = true
						return sentinel
					}
					return nil
				}
				t.Cleanup(func() { testHookPrivateRead = nil })
				err = writer.change(t.Context(), true, func(r *assistantRunRecord) (bool, error) {
					r.Observation.Outcome = run.Observation.Outcome
					return true, nil
				})
				testHookPrivateRead = nil
				if !injected || !errors.Is(err, sentinel) {
					t.Fatal("outcome persistence failure not exercised", err)
				}
				assistantRunMust(t, lease.Close())
				run, err = store.Read(t.Context(), run.Binding, run.ID)
				assistantRunMust(t, err)
				canonicalAfter, _ := json.Marshal(run.Canonical)
				checkpointAfter, _ := json.Marshal(run.Checkpoint)
				if run.Observation.Outcome != nil || run.Checkpoint.Status != "complete" || !bytes.Equal(canonicalBefore, canonicalAfter) || !bytes.Equal(checkpointBefore, checkpointAfter) {
					t.Fatal("lost outcome altered canonical completion")
				}
				expected = f.result(t, run)
				if expected.ObserveSucceeded || expected.Capability == nil || expected.Capability.OK || expected.Capability.Findings != nil {
					t.Fatal("lost normalized outcome invented successful findings")
				}
			}
			before, _ := json.Marshal(run)
			f.s.assistantRunService().close()
			f.s.assistantRunServiceMu.Lock()
			f.s.assistantRunHost = nil
			f.s.assistantRunServiceMu.Unlock()
			duplicate := f.call("POST", "/api/assistant-runs", start)
			if duplicate.Code != 200 {
				t.Fatalf("restart duplicate %d %s", duplicate.Code, duplicate.Body.String())
			}
			if got := f.result(t, run); !reflect.DeepEqual(expected, got) {
				t.Fatal("restart changed normalized result")
			}
			lookup := f.call("GET", "/api/assistant-observations/requests/"+start.RequestID, nil)
			if lookup.Code != 200 {
				t.Fatal("restart lookup", lookup.Code)
			}
			after, err := store.Read(t.Context(), run.Binding, run.ID)
			assistantRunMust(t, err)
			raw, _ := json.Marshal(after)
			if !bytes.Equal(before, raw) || f.calls.Load() != 1 {
				t.Fatal("restart/read/duplicate changed retained run or replayed")
			}
		})
	}
}
