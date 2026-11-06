package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/wire"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

type assistantSettingsObservationExecutor struct{}

func (*assistantSettingsObservationExecutor) assistantObservationProfile(source assistantRunSource) (assistantObservationProfile, error) {
	if source.Start.Observation == nil {
		return assistantObservationProfile{}, errAssistantRunScope
	}
	return assistantSettingsProfile(source.Start.Observation.Kind, source.Start.Observation.Model)
}
func assistantSettingsObservationRun(source assistantRunSource) (*adapter.Run, runcontext.Session, adapter.ObservationOptions, error) {
	fixed, err := (&assistantSettingsObservationExecutor{}).assistantObservationProfile(source)
	if err != nil || source.Connection.Assistant.Endpoint == nil {
		return nil, runcontext.Session{}, adapter.ObservationOptions{}, errAssistantRunScope
	}
	one, off, on := 1, false, true
	family := source.Connection.Assistant.Endpoint.Kind
	input := &runcontext.Input{Phase: "work", Connection: source.Binding.ConnectionID, ConnectionRevisions: map[string]string{source.Binding.ConnectionID: source.Binding.ConnectionRevision}, RequestCap: &one, Memory: &off, ArtifactProposals: &off, CurrentRequest: &runcontext.CurrentRequest{Message: source.Text, Input: source.Text}}
	// Transient measurement capabilities deliberately ignore previous findings.
	caps := &contract.CapabilityEndpointCapabilities{NativeToolCalls: &on, ParallelToolCalls: &on, StrictAlternation: &off, StreamingQuirks: &contract.CapabilityStreamingQuirks{ReasoningField: json.RawMessage(`null`), WholeJSONAnswer: true}}
	run := &adapter.Run{Wire: family, ModelName: fixed.Model, Thinking: "off", Critic: "off", AllowConversation: true, Prompt: source.Text, Context: input, RequestOutputCeiling: fixed.OutputCeiling, Capabilities: caps}
	capabilities, err := wire.CapabilitiesFor(wire.EndpointKind(family), fixed.Model, func(wire.EndpointKind, string) *wire.Value {
		raw, _ := json.Marshal(caps)
		v, _ := wire.Parse(raw)
		return v
	}, nil)
	if err != nil {
		return nil, runcontext.Session{}, adapter.ObservationOptions{}, err
	}
	named := runcontext.RunModels{Critic: fixed.Model, Child: fixed.Model}
	session := runcontext.Session{Prompt: run.Prompt, AllowConversation: true, Critic: "off", Context: input, Model: &runcontext.ModelSession{Family: family, Model: fixed.Model, Thinking: "off", ToolSchemas: string(capabilities.ToolSchemas)}, PolicyLegacyModels: &named, PolicyFamilies: adapter.PolicyFamilies(run, named), PolicyConnections: append(adapter.PolicyConnectionIDs(run), named.Critic, named.Child)}
	if capabilities.ContextWindowTokens != nil && capabilities.OutputTokens != nil {
		session.Model.Budget = &runcontext.ModelBudget{ContextWindowTokens: *capabilities.ContextWindowTokens, OutputTokens: *capabilities.OutputTokens}
	}
	options := adapter.ObservationOptions{}
	if fixed.Kind == "capability" {
		options.Tools = []contract.ToolOfferHostTool{{Name: "probe_echo", Description: "Echo a number.", InputSchema: json.RawMessage(assistantObservationSchema), Taxonomy: contract.ToolTaxonomy{Operation: "transform", Effect: "pure"}}}
	}
	return run, session, options, nil
}
func (*assistantSettingsObservationExecutor) Prepare(ctx context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	_, session, _, err := assistantSettingsObservationRun(source)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	opened, err := runcontext.ContextualSession(ctx, session)
	if err != nil {
		return contract.Checkpoint{}, err
	}
	defer opened.Context.Close()
	return opened.Context.Snapshot("")
}
func (*assistantSettingsObservationExecutor) Execute(ctx context.Context, e *assistantRunExecution) error {
	run, _, options, err := assistantSettingsObservationRun(e.source)
	if err != nil {
		return err
	}
	run.Context.Authorize = e.Authorize
	if err = e.bindObservationInput(ctx, run.Context); err != nil {
		return err
	}
	run.Model, err = e.assistantModel()
	if err != nil {
		return err
	}
	run.Emit = func(event contract.RunEvent) { _ = e.deliverCanonicalEvent(ctx, event) }
	observed, observeErr := eino.Observe(ctx, run, options)
	if attachErr := e.observationError(); attachErr != nil {
		return attachErr
	}
	if observeErr != nil {
		e.closeCanonicalDispatch()
		if err = e.finalizeCanonicalStop(); err != nil {
			return errors.Join(observeErr, err)
		}
	}
	// No outcome is evidence until the exact retained physical response and the
	// package's acknowledged terminal disposition have both been checked.
	persist, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finish()
	latest, err := e.writer.lease.store.Read(persist, e.record.Binding, e.record.ID)
	if err != nil {
		return err
	}
	outcome, err := assistantSettingsOutcome(latest, observed, observeErr)
	if err != nil {
		return errors.Join(observeErr, err)
	}
	if outcome != nil {
		err = e.writer.change(persist, true, func(run *assistantRunRecord) (bool, error) {
			if run.Observation == nil || run.Observation.Outcome != nil || !reflect.DeepEqual(run.Binding, latest.Binding) || !reflect.DeepEqual(run.Canonical, latest.Canonical) || !reflect.DeepEqual(run.Operations, latest.Operations) {
				return false, errAssistantRunConflict
			}
			run.Observation.Outcome = outcome
			return true, validateAssistantSettingsOutcome(*run)
		})
		if err != nil {
			return errors.Join(observeErr, err)
		}
	}
	return observeErr
}

type assistantCapabilityFacts struct {
	NativeToolCalls   bool   `json:"nativeToolCalls"`
	ParallelToolCalls bool   `json:"parallelToolCalls"`
	ReasoningField    string `json:"reasoningField,omitempty"`
}
type assistantCapabilityFindings struct {
	At      string                   `json:"at"`
	Facts   assistantCapabilityFacts `json:"facts"`
	Unknown []string                 `json:"unknown"`
}
type assistantCapabilityOutcome struct {
	OK       bool                         `json:"ok"`
	Findings *assistantCapabilityFindings `json:"findings,omitempty"`
	Problem  string                       `json:"problem,omitempty"`
	Status   *int                         `json:"status,omitempty"`
}
type assistantSettingsObservationResult struct {
	Kind             string                      `json:"kind"`
	ObserveSucceeded bool                        `json:"observeSucceeded"`
	Capability       *assistantCapabilityOutcome `json:"capability,omitempty"`
	Ping             *ProbeResult                `json:"ping,omitempty"`
	Failure          *contract.Failure           `json:"failure,omitempty"`
}
type assistantSettingsObservationOutcome struct {
	OperationID   string                             `json:"operationId"`
	IntentDigest  string                             `json:"intentDigest"`
	ResultDigest  string                             `json:"resultDigest"`
	CanonicalHead assistantCanonicalHead             `json:"canonicalHead"`
	Public        assistantSettingsObservationResult `json:"public"`
}
type assistantSettingsPhysicalIntent struct {
	Suffix  string        `json:"suffix"`
	Request relay.Request `json:"request"`
}
type assistantSettingsPhysicalReceipt struct {
	Status    int         `json:"status"`
	Headers   http.Header `json:"headers"`
	Body      []byte      `json:"body"`
	LatencyMs *int64      `json:"latencyMs,omitempty"`
}

// This reads the adapter's generated body; it never builds another wire body.
func assistantSettingsWire(source assistantRunSource, suffix string, request relay.Request) error {
	if source.Start.Observation == nil || request.Method != "" && request.Method != http.MethodPost {
		return errAssistantRunScope
	}
	fixed, err := assistantSettingsProfile(source.Start.Observation.Kind, source.Binding.Model)
	if err != nil {
		return err
	}
	if source.Start.Observation.Model != source.Binding.Model || !uniqueJSON(request.Body) {
		return errAssistantRunScope
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(request.Body, &body) != nil {
		return errAssistantRunScope
	}
	var model string
	var ceiling int
	var stream bool
	if json.Unmarshal(body["model"], &model) != nil || model != fixed.Model || json.Unmarshal(body["max_tokens"], &ceiling) != nil || ceiling != fixed.OutputCeiling {
		return errAssistantRunScope
	}
	if len(body["stream"]) > 0 && (json.Unmarshal(body["stream"], &stream) != nil || stream) {
		return errAssistantRunScope
	}
	if len(body["max_completion_tokens"]) > 0 || len(body["thinking"]) > 0 || len(body["reasoning_effort"]) > 0 {
		return errAssistantRunScope
	}
	if fixed.Kind == "ping" {
		if source.Connection.Assistant.Endpoint.Kind != "anthropic" || suffix != "v1/messages" || len(body["tools"]) > 0 || len(body["tool_choice"]) > 0 {
			return errAssistantRunScope
		}
	} else {
		if source.Connection.Assistant.Endpoint.Kind != "openai-compatible" || suffix != "chat/completions" {
			return errAssistantRunScope
		}
		var tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if json.Unmarshal(body["tools"], &tools) != nil || len(tools) != 1 || tools[0].Type != "function" || tools[0].Function.Name != "probe_echo" || tools[0].Function.Description != "Echo a number." {
			return errAssistantRunScope
		}
		var got, want any
		if json.Unmarshal(tools[0].Function.Parameters, &got) != nil || json.Unmarshal([]byte(assistantObservationSchema), &want) != nil || !reflect.DeepEqual(got, want) {
			return errAssistantRunScope
		}
	}
	return nil
}
func (d *assistantModelDispatch) settingsPing() bool {
	e := d.execution
	return e != nil && e.observation != nil && e.source.Start.Purpose == assistantSettingsObservationPurpose && e.source.Start.Observation != nil && e.source.Start.Observation.Kind == "ping" && e.observation.custody.Kind == "ping"
}
func assistantSettingsReceipt(run assistantRunRecord) (assistantRunOperation, assistantSettingsPhysicalReceipt, error) {
	var receipt assistantSettingsPhysicalReceipt
	if run.Observation == nil || len(run.Operations) != 1 || !assistantCanonicalActive(run) || run.Checkpoint == nil || run.Checkpoint.Status == "running" || run.Checkpoint.Status == "paused" {
		return assistantRunOperation{}, receipt, errAssistantRunScope
	}
	op := run.Operations[0]
	if op.Kind != "model" || op.Name != run.Binding.Model || op.State != "completed" || len(op.Result) > assistantRunRecordLimit || !uniqueJSON(op.Result) {
		return op, receipt, errAssistantRunScope
	}
	if json.Unmarshal(op.Result, &receipt) != nil || receipt.Status < 100 || receipt.Status > 599 || receipt.LatencyMs == nil || *receipt.LatencyMs < 0 || *receipt.LatencyMs > relayDeadline.Milliseconds() || len(receipt.Body) > assistantRunRecordLimit {
		return op, receipt, errAssistantRunScope
	}
	var intent assistantSettingsPhysicalIntent
	if !uniqueJSON(op.Intent) || json.Unmarshal(op.Intent, &intent) != nil {
		return op, receipt, errAssistantRunScope
	}
	selector := &assistantObservationSelector{run.Observation.Kind, run.Observation.Model}
	family := "openai-compatible"
	if selector.Kind == "ping" {
		family = "anthropic"
	}
	source := assistantRunSource{Start: assistantRunStart{Observation: selector}, Binding: run.Binding, Connection: aiConnection{Assistant: AssistantSlotView{Endpoint: &AssistantEndpointView{Kind: family}}}}
	if assistantSettingsWire(source, intent.Suffix, intent.Request) != nil {
		return op, receipt, errAssistantRunScope
	}
	return op, receipt, nil
}
func assistantSettingsReasoning(body []byte) string {
	if len(body) > assistantRunRecordLimit || !uniqueJSON(body) {
		return ""
	}
	var answer struct {
		Choices []struct {
			Message map[string]json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &answer) != nil || len(answer.Choices) == 0 {
		return ""
	}
	for _, field := range []string{"reasoning_content", "reasoning"} {
		raw := answer.Choices[0].Message[field]
		var value string
		if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && json.Unmarshal(raw, &value) == nil {
			return field
		}
	}
	return ""
}
func assistantSettingsOutcome(run assistantRunRecord, observed adapter.ObservationResult, observeErr error) (*assistantSettingsObservationOutcome, error) {
	op, receipt, err := assistantSettingsReceipt(run)
	if err != nil {
		return nil, nil
	} // uncertain/not-started receipts never establish findings
	success := observeErr == nil
	if success && (run.Checkpoint.Status != "complete" || observed.Checkpoint.ID != run.Canonical.Head.RunID || observed.Receipt.Revision != run.Canonical.Head.Revision || observed.Receipt.CommitID != run.Canonical.Head.CommitID || observed.Receipt.Cursor != run.Canonical.Head.Cursor || observed.Checkpoint.Used != 1 || observed.Checkpoint.Limit != 1) {
		return nil, errAssistantRunScope
	}
	public := assistantSettingsObservationResult{Kind: run.Observation.Kind, ObserveSucceeded: success}
	if observeErr != nil {
		f := failure.Normalize(observeErr, "model", time.Now())
		f.Detail = nil
		f.RequestID = nil
		f.RetryAt = nil
		public.Failure = &f
	}
	if public.Kind == "ping" {
		complete := success
		reachable := receipt.Status >= 200 && receipt.Status < 300
		diagnostic := ""
		if !reachable {
			diagnostic = statusDiagnostic(receipt.Status)
		}
		public.Ping = &ProbeResult{Reachable: reachable, Status: receipt.Status, LatencyMs: *receipt.LatencyMs, Diagnostic: diagnostic, ObservationComplete: &complete}
	} else {
		useful := success || receipt.Status == 400 || receipt.Status == 422
		capability := &assistantCapabilityOutcome{OK: useful}
		if useful {
			facts := assistantCapabilityFacts{}
			if success {
				facts.NativeToolCalls = len(observed.Calls) > 0
				facts.ParallelToolCalls = len(observed.Calls) > 1
				facts.ReasoningField = assistantSettingsReasoning(receipt.Body)
			}
			capability.Findings = &assistantCapabilityFindings{At: time.Now().UTC().Format(time.RFC3339Nano), Facts: facts, Unknown: []string{"context window", "vision", "reasoning", "tool-schemas", "turn order"}}
		} else {
			capability.Problem = "unreadable"
			if receipt.Status < 200 || receipt.Status >= 300 {
				capability.Problem = "refused"
			}
			capability.Status = &receipt.Status
		}
		public.Capability = capability
	}
	out := &assistantSettingsObservationOutcome{OperationID: op.ID, IntentDigest: digestOf(op.Intent), ResultDigest: digestOf(op.Result), CanonicalHead: run.Canonical.Head, Public: public}
	return out, nil
}
func validateAssistantSettingsOutcome(run assistantRunRecord) error {
	if run.Observation == nil || run.Observation.Outcome == nil {
		return nil
	}
	out := run.Observation.Outcome
	op, receipt, err := assistantSettingsReceipt(run)
	if err != nil {
		return err
	}
	if out.OperationID != op.ID || out.IntentDigest != digestOf(op.Intent) || out.ResultDigest != digestOf(op.Result) || !reflect.DeepEqual(out.CanonicalHead, run.Canonical.Head) || out.Public.Kind != run.Observation.Kind {
		return errAssistantRunInvalid
	}
	p := out.Public
	if p.ObserveSucceeded {
		if run.Checkpoint.Status != "complete" || run.Checkpoint.Used != 1 || run.Checkpoint.Limit != 1 || p.Failure != nil || receipt.Status < 200 || receipt.Status >= 300 {
			return errAssistantRunInvalid
		}
	} else {
		if p.Failure == nil {
			return errAssistantRunInvalid
		}
		f, ok := failure.Read(*p.Failure)
		if !ok || f.Detail != nil || f.RequestID != nil || f.RetryAt != nil {
			return errAssistantRunInvalid
		}
	}
	if p.Kind == "ping" {
		if p.Capability != nil || p.Ping == nil || p.Ping.ObservationComplete == nil || *p.Ping.ObservationComplete != p.ObserveSucceeded || p.Ping.Status != receipt.Status || p.Ping.LatencyMs != *receipt.LatencyMs || p.Ping.Reachable != (receipt.Status >= 200 && receipt.Status < 300) {
			return errAssistantRunInvalid
		}
		diagnostic := ""
		if !p.Ping.Reachable {
			diagnostic = statusDiagnostic(receipt.Status)
		}
		if p.Ping.Diagnostic != diagnostic {
			return errAssistantRunInvalid
		}
	} else {
		c := p.Capability
		if p.Ping != nil || c == nil {
			return errAssistantRunInvalid
		}
		useful := p.ObserveSucceeded || receipt.Status == 400 || receipt.Status == 422
		if c.OK != useful {
			return errAssistantRunInvalid
		}
		if useful {
			if c.Findings == nil || c.Problem != "" || c.Status != nil {
				return errAssistantRunInvalid
			}
			if _, err := time.Parse(time.RFC3339Nano, c.Findings.At); err != nil {
				return errAssistantRunInvalid
			}
			if !reflect.DeepEqual(c.Findings.Unknown, []string{"context window", "vision", "reasoning", "tool-schemas", "turn order"}) {
				return errAssistantRunInvalid
			}
			facts := c.Findings.Facts
			if p.ObserveSucceeded {
				count, err := assistantSettingsCallCount(receipt.Body)
				if err != nil || facts.NativeToolCalls != (count > 0) || facts.ParallelToolCalls != (count > 1) {
					return errAssistantRunInvalid
				}
			}
			if !p.ObserveSucceeded && (facts.NativeToolCalls || facts.ParallelToolCalls || facts.ReasoningField != "") {
				return errAssistantRunInvalid
			}
			if facts.ParallelToolCalls && !facts.NativeToolCalls || facts.ReasoningField != "" && (!contains([]string{"reasoning_content", "reasoning"}, facts.ReasoningField) || facts.ReasoningField != assistantSettingsReasoning(receipt.Body)) {
				return errAssistantRunInvalid
			}
		} else if c.Findings != nil || c.Status == nil || *c.Status != receipt.Status || !contains([]string{"refused", "unreadable"}, c.Problem) {
			return errAssistantRunInvalid
		}
	}
	return nil
}

// Failed/uncertain attempts have no provider evidence; reads remain model-free.
func assistantSettingsUnavailableResult(run assistantRunRecord) assistantSettingsObservationResult {
	p := assistantSettingsObservationResult{Kind: run.Observation.Kind}
	if p.Kind == "ping" {
		complete := false
		p.Ping = &ProbeResult{Diagnostic: DiagnosticUnexpected, ObservationComplete: &complete}
	} else {
		p.Capability = &assistantCapabilityOutcome{Problem: "unreachable"}
	}
	return p
}

// No provider prose or raw physical/canonical body is projected into Settings.
func assistantSettingsPublic(run assistantRunRecord) assistantRunView {
	return assistantRunView{AdmissionID: run.AdmissionID, ID: run.ID, Revision: run.Revision, Owner: run.Binding.Owner, RequestID: run.Binding.RequestID, State: run.State, Historical: run.Historical, Operations: []assistantRunOperation{}, Events: []assistantRunEvent{}, Cursor: assistantRunCursor(run)}
}

func relayObservationRequest(r *http.Request, body []byte) relay.Request {
	return relay.Request{Method: r.Method, Body: body}
}

func assistantSettingsCallCount(body []byte) (int, error) {
	if !uniqueJSON(body) {
		return 0, errAssistantRunInvalid
	}
	var answer struct {
		Choices []struct {
			Message struct {
				Calls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &answer) != nil || len(answer.Choices) == 0 {
		return 0, errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, call := range answer.Choices[0].Message.Calls {
		var args map[string]json.RawMessage
		if call.ID == "" || seen[call.ID] || call.Type != "function" || call.Function.Name != "probe_echo" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args == nil {
			return 0, errAssistantRunInvalid
		}
		seen[call.ID] = true
	}
	return len(answer.Choices[0].Message.Calls), nil
}
