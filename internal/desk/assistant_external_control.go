package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// Private physical refusal custody. Neither this record nor its public display
// projection is a successful Agent result, source receipt, or dispatch grant.
type assistantExternalControl struct {
	Version             int    `json:"version"`
	Kind                string `json:"kind"`
	HostOperationID     string `json:"hostOperationId"`
	SourceBindingDigest string `json:"sourceBindingDigest"`
	RequestID           string `json:"requestId"`
	Query               string `json:"query,omitempty"`
	Provider            string `json:"provider,omitempty"`
	Reason              string `json:"reason"`
}

// Reject present-empty optional members as well as aliases/null/duplicates.
// Their absence must remain distinct from a caller-supplied empty value.
func (c *assistantExternalControl) UnmarshalJSON(raw []byte) error {
	type plain assistantExternalControl
	var held plain
	if _, err := assistantChatValue(raw); err != nil {
		return errAssistantRunInvalid
	}
	if err := readAssistantHostReceipt(raw, &held); err != nil {
		return err
	}
	fields, err := configMembers(raw)
	if err != nil {
		return errAssistantRunInvalid
	}
	for _, field := range fields {
		if field.name == "query" && (held.Query == "" || runcontext.U16Length(held.Query) > 2000) || field.name == "provider" && !researchSourceName.MatchString(held.Provider) {
			return errAssistantRunInvalid
		}
	}
	*c = assistantExternalControl(held)
	return nil
}

// Only an explicit producer exit BEFORE acquisition may supply this witness.
// An absent custody row, error text, or IsError bit cannot create one.
type assistantExternalNotStarted struct{ kind, reason, query, provider string }

func assistantExternalReadName(name string) bool {
	return name == assistantSearchTool || name == assistantLinkTool || name == assistantWebsiteTool
}
func assistantExternalSourced(ctx context.Context, name string) bool {
	return assistantExternalReadName(name) && ctx.Value(assistantHostSourceKey{}) != nil
}
func assistantExternalFailure(err error, notStarted bool) error {
	f := failure.Normalize(err, "tool", time.Now())
	f.Outcome = "unknown"
	if notStarted {
		f.Outcome = "not-started"
	}
	return failure.NewError(f)
}
func assistantExternalInputFailure(ctx context.Context, name string, err error) (runcontext.ToolResult, error) {
	if assistantExternalSourced(ctx, name) {
		return runcontext.ToolResult{}, assistantExternalFailure(err, true)
	}
	return assistantHostFailure(err), nil
}
func assistantExternalReason(kind, reason string) bool {
	switch kind {
	case "research-choice":
		return reason == "choice-required"
	case "provided-only":
		return reason == "provided-only"
	case "setup-required":
		return reason == "configuration-required" || reason == "connection-required" || reason == "verifier-required"
	}
	return false
}
func assistantExternalControlFailure(c assistantExternalControl) error {
	code := "permission"
	if c.Kind == "setup-required" {
		code = "configuration"
	}
	return failure.Err(code, "desk", "not-started")
}

// The explicit early-exit witness is checked again in the SAME transaction
// which retains the physical control. No lock spans producer or network work.
func assistantExternalNoAcquisition(run assistantRunRecord, host string) bool {
	for _, v := range run.Searches {
		if v.HostOperationID == host && (v.Acquire != "reserved" || v.Seal != "not-started" || v.DispatchClosed || v.Stage.Intent != nil || v.Stage.Receipt != nil || v.Original.Intent != nil || v.Original.Receipt != nil || v.SealStage.Intent != nil || v.SealStage.Receipt != nil || v.Final.Intent != nil || v.Final.Receipt != nil || v.Digest != "") {
			return false
		}
	}
	materialStarted := func(v assistantLinkCustody) bool {
		return v.HostOperationID == host && (v.Acquire != "reserved" || v.Seal != "not-started" || v.DispatchClosed || v.Stage.Intent != nil || v.Stage.Receipt != nil || v.Original.Intent != nil || v.Original.Receipt != nil || v.SealStage.Intent != nil || v.SealStage.Receipt != nil || v.Final.Intent != nil || v.Final.Receipt != nil || v.Digest != "")
	}
	for _, v := range run.Links {
		if materialStarted(v) {
			return false
		}
	}
	for _, v := range run.Websites {
		if materialStarted(assistantLinkCustody(v)) {
			return false
		}
	}
	return true
}
func assistantExternalControlFor(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent, witness *assistantExternalNotStarted) (*assistantExternalControl, error) {
	if witness == nil {
		return nil, nil
	}
	if in.SourceBinding == nil || !assistantExternalReadName(op.Name) || !assistantExternalReason(witness.kind, witness.reason) || !assistantExternalNoAcquisition(run, op.ID) {
		return nil, errAssistantRunInvalid
	}
	if _, ok := assistantHostSourceRecord(in.SourceBinding, op.Name); !ok {
		return nil, errAssistantRunInvalid
	}
	c := &assistantExternalControl{Version: 1, Kind: witness.kind, HostOperationID: op.ID, SourceBindingDigest: digestOf(assistantChatJSON(in.SourceBinding)), RequestID: run.Binding.RequestID, Query: witness.query, Provider: witness.provider, Reason: witness.reason}
	if !assistantExternalControlShape(*c, op.Name, in.Arguments) {
		return nil, errAssistantRunInvalid
	}
	return c, nil
}
func assistantExternalControlShape(c assistantExternalControl, name string, args []byte) bool {
	if c.Version != 1 || !assistantExternalReason(c.Kind, c.Reason) || !assistantRunText(c.HostOperationID, 128) || !assistantRunDigest(c.SourceBindingDigest) || !assistantRunText(c.RequestID, 128) || c.Provider != "" && !researchSourceName.MatchString(c.Provider) {
		return false
	}
	if name == assistantSearchTool {
		query, err := readAssistantSearchQuery(args)
		return err == nil && query == c.Query
	}
	return (name == assistantLinkTool || name == assistantWebsiteTool) && c.Query == "" && c.Provider == ""
}
func assistantExternalRetainedFailure(raw []byte) (*failure.Error, bool) {
	var f contract.Failure
	if !uniqueJSON(raw) || json.Unmarshal(raw, &f) != nil {
		return nil, false
	}
	safe, ok := failure.Read(f)
	if !ok {
		return nil, false
	}
	return failure.NewError(safe), true
}
func validateAssistantExternalResult(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent, result assistantHostResult) error {
	if len(result.ExternalFailure) == 0 && result.ExternalControl == nil {
		return nil
	}
	if in.SourceBinding == nil || !assistantExternalReadName(op.Name) || op.State != "refused" || result.Read != nil || result.Tests != nil || result.DialectRead != nil || result.PolicySource != nil {
		return errAssistantRunInvalid
	}
	if _, ok := assistantHostSourceRecord(in.SourceBinding, op.Name); !ok {
		return errAssistantRunInvalid
	}
	held, ok := assistantExternalRetainedFailure(result.ExternalFailure)
	if !ok {
		return errAssistantRunInvalid
	}
	parsed, err := runcontext.ParseJSON(result.Result)
	if err != nil || !runcontext.ReadToolResult(parsed).Err() || !assistantCanonicalHostResultWireEqual(result.Result, assistantHostFailure(held).Value().JSON()) {
		return errAssistantRunInvalid
	}
	if c := result.ExternalControl; c != nil {
		if !assistantExternalControlShape(*c, op.Name, in.Arguments) || c.HostOperationID != op.ID || c.RequestID != run.Binding.RequestID || c.SourceBindingDigest != digestOf(assistantChatJSON(in.SourceBinding)) || !assistantExternalNoAcquisition(run, op.ID) || held.Failure != assistantExternalControlFailure(*c).(*failure.Error).Failure {
			return errAssistantRunInvalid
		}
	} else if held.Failure.Outcome != "unknown" {
		return errAssistantRunInvalid
	}
	if in.DurableDispatchID != "" {
		d := assistantCanonicalRetainedHost(run, op.Name, in)
		if d == nil || d.State != "claimed" && d.State != "unknown" {
			return errAssistantRunInvalid
		}
		if d.State == "unknown" && (d.Failure == nil || !assistantCanonicalHostResultWireEqual(assistantChatJSON(d.Failure), result.ExternalFailure)) {
			return errAssistantRunInvalid
		}
	}
	return nil
}

// Finite authenticated mapping for DISPLAY only. Canonical events retain their
// original wire names and source records; no prefix is stripped from any input.
type assistantExternalCallView struct {
	CallID          string                    `json:"callId"`
	HostOperationID string                    `json:"hostOperationId"`
	OwnTool         string                    `json:"ownTool"`
	WireName        string                    `json:"wireName"`
	Control         *assistantExternalControl `json:"control,omitempty"`
	Outcome         string                    `json:"outcome,omitempty"`
}

func assistantExternalCalls(run assistantRunRecord) []assistantExternalCallView {
	var out []assistantExternalCallView
	for _, op := range run.Operations {
		var in assistantHostIntent
		if op.Kind != "host" || readAssistantHostReceipt(op.Intent, &in) != nil || in.SourceBinding == nil || !assistantExternalReadName(op.Name) || !assistantNativeHostCall(run, op.Name, in) || in.Root && !assistantParentHostCall(run, op.Name, in) {
			continue
		}
		if _, ok := assistantHostSourceRecord(in.SourceBinding, op.Name); !ok {
			continue
		}
		view := assistantExternalCallView{CallID: in.CallID, HostOperationID: op.ID, OwnTool: op.Name, WireName: in.SourceBinding.WireName}
		if op.Result != nil {
			var result assistantHostResult
			if readAssistantHostReceipt(op.Result, &result) != nil || validateAssistantExternalResult(run, op, in, result) != nil {
				continue
			}
			view.Control = result.ExternalControl
			if held, ok := assistantExternalRetainedFailure(result.ExternalFailure); ok {
				view.Outcome = held.Failure.Outcome
			}
		}
		out = append(out, view)
	}
	return out
}
func assistantExternalDisplayName(run assistantRunRecord, callID, name string) string {
	for _, row := range assistantExternalCalls(run) {
		if row.CallID == callID && row.WireName == name {
			return row.OwnTool
		}
	}
	return name
}
func assistantExternalControlItem(item map[string]any, c assistantExternalControl) {
	item["externalControl"] = c
	item["status"] = "failed"
	if c.Kind == "research-choice" && item["name"] != assistantLinkTool {
		item["researchSuggestion"] = true
	}
	if c.Query != "" {
		item["search"] = assistantSearchStep{Query: c.Query, Provider: c.Provider}
	}
	if c.Kind == "setup-required" && item["name"] == assistantSearchTool {
		item["failure"] = "setup-required"
	}
}
func assistantExternalControlMessage(c assistantExternalControl) string {
	switch c.Kind {
	case "research-choice":
		return "Choose research to search external sources. No research started for this operation."
	case "provided-only":
		return "This request uses only the conversation and supplied sources. No research started for this operation."
	default:
		return "External sources need configuration. No research started for this operation."
	}
}

// Retained failures are immutable. Replaying one never becomes a nil-error
// ToolResult and never re-enters the producer or source acquisition.
func assistantExternalReplay(result assistantHostResult) (error, bool) {
	if len(result.ExternalFailure) == 0 {
		return nil, false
	}
	held, ok := assistantExternalRetainedFailure(result.ExternalFailure)
	if !ok {
		return errAssistantRunInvalid, true
	}
	return held, true
}

// Keep exact closed failure bytes rather than mapping arbitrary caught prose.
func assistantExternalFailureBytes(err error) []byte {
	f := failure.Normalize(err, "tool", time.Now())
	return bytes.Clone(assistantChatJSON(f))
}

func assistantExternalObservationFailure(ctx context.Context, name string, result runcontext.ToolResult, cause error) (assistantHostObservation, error) {
	if assistantExternalSourced(ctx, name) {
		return assistantHostObservation{Result: result}, assistantExternalFailure(cause, false)
	}
	return assistantHostObservation{Result: result}, nil
}
func assistantExternalControlObservation(ctx context.Context, name string, result runcontext.ToolResult, kind, reason, query, provider string) (assistantHostObservation, error) {
	if !assistantExternalSourced(ctx, name) {
		return assistantHostObservation{Result: result}, nil
	}
	witness := &assistantExternalNotStarted{kind: kind, reason: reason, query: query, provider: provider}
	return assistantHostObservation{Result: result, ExternalNotStarted: witness}, assistantExternalControlFailure(assistantExternalControl{Kind: kind})
}

func assistantExternalResultError(result runcontext.ToolResult) error {
	if result.Err() && result.StructuredContent != nil {
		var payload struct {
			Failure *contract.Failure `json:"failure"`
		}
		if json.Unmarshal(result.StructuredContent.JSON(), &payload) == nil && payload.Failure != nil {
			if held, ok := failure.Read(*payload.Failure); ok {
				return failure.NewError(held)
			}
		}
	}
	return failure.Err("invalid-response", "desk", "unknown")
}
