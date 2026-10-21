package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// Only this finite domain payload is absent from canonical Agent events. It is
// authored by a finite domain producer with its exact retained result.
type assistantDomainOutput struct {
	DialectAction        *assistantDialectActionOutput `json:"dialectAction,omitempty"`
	TestProposal         *assistantTestProposalOutput  `json:"testProposal,omitempty"`
	Recipe               *assistantDialectRecipeOutput `json:"dialectRecipe,omitempty"`
	Kind                 string                        `json:"kind"`
	RunID                string                        `json:"runId"`
	RequestKey           string                        `json:"requestKey"`
	OperationID          string                        `json:"operationId"`
	CallID               string                        `json:"callId"`
	Draft                graphDraft                    `json:"draft,omitzero"`
	PolicyUpdate         *assistantPolicyProposal      `json:"policyUpdate,omitempty"`
	PolicyInterpretation *policyInterpretationProposal `json:"policyInterpretation,omitempty"`
	PolicySource         *policyAuthoringSource        `json:"policySource,omitempty"`
}
type assistantGraphRead struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Declaration string `json:"declaration"`
}
type assistantHostIntent struct {
	DialectAction  *assistantDialectActionMetadata `json:"dialectAction,omitempty"`
	RecipeProposal *assistantDialectRecipeProposal `json:"recipeProposal,omitempty"`
	CallID         string                          `json:"callId"`
	CallbackID     string                          `json:"callbackId,omitempty"`
	Root           bool                            `json:"root"`
	Arguments      []byte                          `json:"arguments"`
	Policy         *assistantPolicyHostBinding     `json:"policy,omitempty"`
}
type assistantHostResult struct {
	DialectRead  *assistantDialectRead  `json:"dialectRead,omitempty"`
	Tests        *assistantTestRead     `json:"tests,omitempty"`
	Result       []byte                 `json:"result"`
	Read         *assistantGraphRead    `json:"read,omitempty"`
	PolicySource *policyAuthoringSource `json:"policySource,omitempty"`
}

func assistantGraphTool(name string) bool {
	return contains([]string{"list_decisions", "read_decision", "get_graph_authoring_instructions", "graph_validate", "graph_explain", "propose_graph", "graph_rehearse"}, name)
}

// Arguments remain exact bytes in host intents. Callback/event JSON encodes
// RawMessage with compaction and HTML escaping; compare that same wire form,
// without decoding facts into maps or changing number spellings or key order.
func assistantHostArgumentsMatch(retained, original []byte) bool {
	if !json.Valid(retained) || !json.Valid(original) {
		return false
	}
	left, err := json.Marshal(json.RawMessage(retained))
	if err != nil {
		return false
	}
	right, err := json.Marshal(json.RawMessage(original))
	return err == nil && bytes.Equal(left, right)
}

func assistantParentHostCall(run assistantRunRecord, name string, intent assistantHostIntent) bool {
	count := 0
	for _, event := range run.Events {
		if c := event.Event.ToolCall; c != nil && c.CallID != nil && *c.CallID == intent.CallID {
			if c.Name != name || !assistantHostArgumentsMatch(c.Args, intent.Arguments) {
				return false
			}
			count++
		}
	}
	return count == 1
}
func assistantNativeHostCall(run assistantRunRecord, name string, intent assistantHostIntent) bool {
	if intent.CallbackID == "" {
		return true
	}
	// Native account admission serializes author, child and critic invocations.
	// A callback is appended while its owning model invocation still holds that
	// account. Later model operations must not rebind an earlier callback.
	invocation := ""
	for _, op := range run.Operations {
		if op.Kind == "model" {
			invocation = op.ID
		}
		if op.ID == intent.CallbackID && op.Kind == "callback" {
			var v struct {
				Invocation string `json:"invocation"`
				Call       struct {
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"call"`
			}
			if json.Unmarshal(op.Intent, &v) != nil || invocation == "" || v.Invocation != invocation || v.Call.ID != intent.CallID || v.Call.Name != op.Name || !assistantHostArgumentsMatch(v.Call.Arguments, intent.Arguments) {
				return false
			}
			for _, inv := range run.Operations {
				if inv.ID == v.Invocation && inv.Kind == "model" {
					var request adapter.AgentRequest
					if json.Unmarshal(inv.Intent, &request) != nil {
						return false
					}
					for _, tool := range request.Tools {
						if tool.Name == v.Call.Name && strings.HasPrefix(tool.Description, name+":") {
							return true
						}
					}
				}
			}
		}
	}
	return false
}
func validateAssistantDomainOutputs(run assistantRunRecord) error {
	receipts := map[string]assistantHostResult{}
	intents := map[string]assistantHostIntent{}
	for _, op := range run.Operations {
		if op.Kind == "host" && !assistantDialectTool(op.Name) && (assistantDialectReserved(op.Intent, "dialectAction") || assistantDialectReserved(op.Result, "dialectRead")) {
			return errAssistantRunInvalid
		}
		if op.Kind == "host" && !assistantDialectRecipeTool(op.Name) && assistantDialectRecipeMetadata(op.Intent) {
			return errAssistantRunInvalid
		}
		if op.Kind == "host" && !assistantTestTool(op.Name) && assistantTestReceiptMetadata(op.Result) {
			return errAssistantRunInvalid
		}
		if op.Kind != "host" || !assistantGraphTool(op.Name) && !assistantPolicyTool(op.Name) && !assistantDialectRecipeTool(op.Name) && !assistantTestTool(op.Name) && !assistantDialectTool(op.Name) {
			continue
		}
		var in assistantHostIntent
		if readAssistantHostReceipt(op.Intent, &in) != nil || !assistantRunText(in.CallID, 256) || !json.Valid(in.Arguments) || !assistantNativeHostCall(run, op.Name, in) || in.Root && !assistantParentHostCall(run, op.Name, in) || assistantHostProposal(op.Name) && !in.Root {
			return errAssistantRunInvalid
		}
		if assistantDialectTool(op.Name) {
			if validateAssistantDialectIntent(run, op, in) != nil {
				return errAssistantRunInvalid
			}
		} else if in.DialectAction != nil {
			return errAssistantRunInvalid
		}
		if assistantDialectRecipeTool(op.Name) {
			if validateAssistantDialectRecipeIntent(run, op, in) != nil || in.Policy != nil {
				return errAssistantRunInvalid
			}
		} else if in.RecipeProposal != nil {
			return errAssistantRunInvalid
		}
		if assistantPolicyTool(op.Name) {
			if in.Policy == nil || !validAssistantPolicyHostBinding(*in.Policy) {
				return errAssistantRunInvalid
			}
		} else if in.Policy != nil {
			return errAssistantRunInvalid
		}
		var argumentErr error
		if assistantDialectTool(op.Name) {
			_, argumentErr = readAssistantDialectArguments(op.Name, in.Arguments)
		} else if assistantPolicyTool(op.Name) {
			_, argumentErr = readAssistantPolicyArguments(op.Name, in.Arguments)
		} else if assistantTestTool(op.Name) {
			_, argumentErr = readAssistantTestArguments(op.Name, in.Arguments)
		} else if assistantDialectRecipeTool(op.Name) {
			_, argumentErr = readAssistantDialectRecipeArguments(op.Name, in.Arguments)
		} else {
			_, argumentErr = readAssistantGraphArguments(op.Name, in.Arguments)
		}
		if argumentErr != nil {
			return errAssistantRunInvalid
		}
		// An invocation/call is one immutable operation, even across reconstruction.
		key := in.CallbackID + "\x00" + in.CallID
		if _, ok := intents[key]; ok {
			return errAssistantRunInvalid
		}
		intents[key] = in
		if op.Result == nil {
			continue
		}
		var result assistantHostResult
		if readAssistantHostReceipt(op.Result, &result) != nil || !json.Valid(result.Result) {
			return errAssistantRunInvalid
		}
		value, err := runcontext.ParseJSON(result.Result)
		if err != nil || !value.IsObject() {
			return errAssistantRunInvalid
		}
		tool := runcontext.ReadToolResult(value)
		if err := validateAssistantDialectReadReceipt(op, in, result); err != nil {
			return err
		}
		if (op.Name == "read_dialect" || op.Name == "read_dialect_publication") && op.State == "completed" && !tool.Err() && result.DialectRead == nil {
			return errAssistantRunInvalid
		}
		if err := validateAssistantTestReadReceipt(op, in, result); err != nil {
			return err
		}
		if op.Name == "read_pack_tests" && op.State == "completed" && !tool.Err() && result.Tests == nil {
			return errAssistantRunInvalid
		}
		if result.PolicySource != nil && (!contains([]string{"propose_policy_pack_interpretation", "propose_policy_graph_interpretation"}, op.Name) || tool.Err() || !validAssistantPolicySource(*result.PolicySource)) {
			return errAssistantRunInvalid
		}
		if result.Read != nil {
			var args struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			var read struct {
				Type    string `json:"type"`
				ID      string `json:"id"`
				Content string `json:"content"`
				SHA256  string `json:"sha256"`
			}
			if op.Name != "read_decision" || !in.Root || tool.Err() || len(tool.Content) != 1 || json.Unmarshal(in.Arguments, &args) != nil || args.Type != "graph" || json.Unmarshal([]byte(tool.Content[0].Text()), &read) != nil || read.Type != "graph" || read.ID != args.ID || result.Read.ID != args.ID || digestOf([]byte(read.Content)) != result.Read.SHA256 || read.SHA256 != result.Read.SHA256 || !assistantRunDigest(result.Read.Declaration) || result.Read.Path == "" {
				return errAssistantRunInvalid
			}
		}
		receipts[op.ID] = result
	}
	seen := map[string]bool{}
	draftIDs := map[string]bool{}
	for _, out := range run.DomainOutputs {
		if out.Kind == "dialect-action" {
			if seen[out.OperationID] || validateAssistantDialectOutput(run, out, receipts) != nil {
				return errAssistantRunInvalid
			}
			seen[out.OperationID] = true
			continue
		}
		if out.DialectAction != nil {
			return errAssistantRunInvalid
		}
		if out.Kind == "test-proposal" {
			if seen[out.OperationID] || validateAssistantTestOutput(run, out, receipts) != nil {
				return errAssistantRunInvalid
			}
			seen[out.OperationID] = true
			continue
		}
		if out.Kind == "dialect-recipe" {
			if seen[out.OperationID] || validateAssistantDialectRecipeOutput(run, out, receipts) != nil {
				return errAssistantRunInvalid
			}
			seen[out.OperationID] = true
			continue
		}
		if out.Recipe != nil {
			return errAssistantRunInvalid
		}
		if out.Kind != "graph" {
			if seen[out.OperationID] || validateAssistantPolicyOutput(run, out, receipts) != nil {
				return errAssistantRunInvalid
			}
			seen[out.OperationID] = true
			continue
		}
		if out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || out.TestProposal != nil {
			return errAssistantRunInvalid
		}
		if out.Kind != "graph" || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey || seen[out.OperationID] || draftIDs[out.Draft.DraftID] || out.Draft.OriginChatID != "" || out.Draft.PolicySources != nil || out.Draft.Saved || out.Draft.WritePending {
			return errAssistantRunInvalid
		}
		seen[out.OperationID] = true
		draftIDs[out.Draft.DraftID] = true
		var producer *assistantRunOperation
		for i := range run.Operations {
			if run.Operations[i].ID == out.OperationID {
				producer = &run.Operations[i]
			}
		}
		if producer == nil || producer.Name != "propose_graph" || producer.Kind != "host" || producer.State != "completed" {
			return errAssistantRunInvalid
		}
		var in assistantHostIntent
		_ = readAssistantHostReceipt(producer.Intent, &in)
		var args struct {
			ID          string `json:"id"`
			Content     string `json:"content"`
			Description string `json:"description"`
		}
		if json.Unmarshal(in.Arguments, &args) != nil || !in.Root || in.CallID != out.CallID || out.Draft.ID != args.ID || out.Draft.Content != args.Content || !assistantGraphDocument(out.Draft.Content) || out.Draft.Description != args.Description {
			return errAssistantRunInvalid
		}
		result, ok := receipts[producer.ID]
		if !ok {
			return errAssistantRunInvalid
		}
		expected, err := assistantGraphProposalResult(run.Binding.Owner.ID, out.Draft)
		if err != nil || !bytes.Equal(result.Result, expected.Value().JSON()) {
			return errAssistantRunInvalid
		}

		if path, err := wireRelativePath(out.Draft.Path); err != nil || path != out.Draft.Path {
			return errAssistantRunInvalid
		}
		if out.Draft.BaseSHA256 != "" {
			found := false
			for _, earlier := range run.Operations {
				if earlier.ID == producer.ID {
					break
				}
				if receipt, ok := receipts[earlier.ID]; ok && receipt.Read != nil && receipt.Read.ID == out.Draft.ID && receipt.Read.Path == out.Draft.Path && receipt.Read.SHA256 == out.Draft.BaseSHA256 {
					found = true
				}
			}
			if !found {
				return errAssistantRunInvalid
			}
		} else if out.Draft.Path != out.Draft.ID+".graph.json" {
			return errAssistantRunInvalid
		}
		draft := out.Draft
		draft.OriginChatID = run.Binding.Owner.ID
		raw, _ := json.Marshal(graphDraftDocument{Version: 1, Drafts: []graphDraft{draft}, Deleted: []string{}})
		if validateGraphDrafts(raw) != nil {
			return errAssistantRunInvalid
		}
		if _, err := time.Parse(time.RFC3339Nano, draft.CreatedAt); err != nil {
			return errAssistantRunInvalid
		}
	}
	counts := map[string]int{}
	for _, out := range run.DomainOutputs {
		counts[out.Kind]++
		if counts[out.Kind] > 16 {
			return errAssistantRunInvalid
		}
	}
	if len(run.DomainOutputs) > 64 {
		return errAssistantRunInvalid
	}
	// A successful producer cannot lose its domain payload on import/recovery.
	for _, op := range run.Operations {
		if op.Kind == "host" && (op.Name == "propose_dialect" || op.Name == "propose_dialect_publication") && !seen[op.ID] {
			if op.State == "completed" || assistantDialectResultHasAction(receipts[op.ID].Result) {
				return errAssistantRunInvalid
			}
		}
		if op.Kind == "host" && op.Name == "propose_dialect_recipe" && op.State == "completed" && !seen[op.ID] {
			return errAssistantRunInvalid
		}
		if op.Kind == "host" && op.Name == "propose_graph" && op.State == "completed" && !seen[op.ID] {
			return errAssistantRunInvalid
		}
		if op.Kind == "host" && op.Name == "propose_test_cases" && op.State == "completed" && !seen[op.ID] {
			return errAssistantRunInvalid
		}
		if op.Kind == "host" && assistantPolicyTool(op.Name) && assistantHostProposal(op.Name) && !seen[op.ID] {
			if op.State == "completed" {
				return errAssistantRunInvalid
			}
			if receipt, ok := receipts[op.ID]; ok && assistantPolicyStaleResult(receipt.Result) {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}

// invokeHost is the sole concrete registry dispatch. Receipt identity is the
// canonical call and (for native agents) the existing durable callback identity.
// Notification-only observation fence for cancellation fixtures. Copies cannot
// replace the producer result or establish authority.
var testHookAssistantGraphObserved func(runcontext.ToolResult, graphDraft)

// Notification only: tests can cancel/fail persistence after actual Policy work.
// It supplies no result, source, identity or authority to the producer.
var testHookAssistantPolicyObserved func()

// Notification only, after actual test validation and before durable retention.
var testHookAssistantTestsObserved func()

func (e *assistantRunExecution) invokeHost(ctx context.Context, name string, args json.RawMessage, work func(context.Context, bool) (runcontext.ToolResult, *assistantGraphRead, *graphDraft, error)) (runcontext.ToolResult, error) {
	return e.invokeDomainHost(ctx, name, args, func(ctx context.Context, root bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error) {
		result, read, draft, err := work(ctx, root)
		if draft == nil {
			return result, read, nil, err
		}
		return result, read, &assistantDomainOutput{Kind: "graph", Draft: *draft}, err
	})
}

type assistantHostObservation struct {
	Result      runcontext.ToolResult
	GraphRead   *assistantGraphRead
	TestsRead   *assistantTestRead
	DialectRead *assistantDialectRead
	Output      *assistantDomainOutput
}

func (e *assistantRunExecution) invokeDomainHost(ctx context.Context, name string, args json.RawMessage, work func(context.Context, bool) (runcontext.ToolResult, *assistantGraphRead, *assistantDomainOutput, error)) (runcontext.ToolResult, error) {
	return e.invokeObservedHost(ctx, name, args, func(ctx context.Context, root bool) (assistantHostObservation, error) {
		result, read, output, err := work(ctx, root)
		return assistantHostObservation{Result: result, GraphRead: read, Output: output}, err
	})
}

func (e *assistantRunExecution) invokeObservedHost(ctx context.Context, name string, args json.RawMessage, work func(context.Context, bool) (assistantHostObservation, error)) (runcontext.ToolResult, error) {
	refuse := func(err error) (runcontext.ToolResult, error) {
		if assistantDialectTool(name) {
			return assistantDialectFailure(err), nil
		}
		if assistantDialectRecipeTool(name) {
			return assistantDialectRecipeFailure(err), nil
		}
		return assistantHostFailure(err), nil
	}
	if err := e.Authorize(ctx); err != nil {
		return refuse(err)
	}
	root, _ := ctx.Value(assistantHostRootKey{}).(bool)
	callback, _ := ctx.Value(assistantHostCallbackKey{}).(string)
	if (e.source.Connection.Assistant.Engine == "codex") != (callback != "") {
		return refuse(errAssistantRunScope)
	}
	in := assistantHostIntent{CallID: runcontext.CallIDOf(ctx), CallbackID: callback, Root: root, Arguments: bytes.Clone(args)}
	if assistantPolicyTool(name) {
		scope, err := readAssistantPolicyScope(e.source)
		if err != nil || scope == nil || scope.ID == "" {
			return refuse(errAssistantRunScope)
		}
		in.Policy = &assistantPolicyHostBinding{ID: scope.ID, Request: scope.Request}
	}
	if !assistantRunText(in.CallID, 256) || assistantHostProposal(name) && (!root || e.record.Binding.Phase == "plan") {
		return refuse(failure.ToolInput("This host proposal is not available in this scope."))
	}
	raw, _ := json.Marshal(in)
	id := "graph-host-" + digestOf(raw)[:32]
	if assistantTestTool(name) {
		id = "test-host-" + digestOf(raw)[:32]
	}
	if assistantPolicyTool(name) {
		id = "policy-host-" + digestOf(raw)[:32]
	}
	if assistantDialectRecipeTool(name) {
		id = assistantDialectRecipeOperationID(in)
	}
	if assistantDialectTool(name) {
		id = assistantDialectOperationID(in)
	}
	op := assistantRunOperation{ID: id, CallID: id, Kind: "host", Name: name, Intent: raw}
	// Scope checks and intent creation are one short transaction. No lock spans work.
	var replay *assistantHostResult
	err := e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if !assistantNativeHostCall(*run, name, in) || root && !assistantParentHostCall(*run, name, in) {
			return false, errAssistantRunScope
		}
		for _, held := range run.Operations {
			if held.ID == id {
				if name == "propose_dialect" || name == "propose_dialect_publication" {
					var original assistantHostIntent
					if readAssistantHostReceipt(held.Intent, &original) != nil || validateAssistantDialectIntent(*run, held, original) != nil {
						return false, errAssistantRunConflict
					}
					base := original
					base.DialectAction = nil
					encoded, _ := json.Marshal(base)
					if !bytes.Equal(encoded, raw) {
						return false, errAssistantRunConflict
					}
					in = original
					raw = held.Intent
				}
				if name == "propose_dialect_recipe" {
					var original assistantHostIntent
					if readAssistantHostReceipt(held.Intent, &original) != nil || validateAssistantDialectRecipeIntent(*run, held, original) != nil {
						return false, errAssistantRunConflict
					}
					base := original
					base.RecipeProposal = nil
					encoded, _ := json.Marshal(base)
					if !bytes.Equal(encoded, raw) {
						return false, errAssistantRunConflict
					}
					in = original
					raw = held.Intent
				}
				if held.Name != name || !bytes.Equal(held.Intent, raw) {
					return false, errAssistantRunConflict
				}
				if held.State != "completed" && held.State != "refused" {
					return false, errAssistantRunDispatch
				}
				var result assistantHostResult
				if readAssistantHostReceipt(held.Result, &result) != nil {
					return false, errAssistantRunInvalid
				}
				replay = &result
				return false, nil
			}
		}
		if name == "propose_dialect" || name == "propose_dialect_publication" {
			var source struct {
				Actions []json.RawMessage `json:"inputActions"`
			}
			if json.Unmarshal(e.source.Original, &source) != nil {
				return false, errAssistantRunInvalid
			}
			if err := prepareAssistantDialectAction(*run, name, &in, len(source.Actions)); err != nil {
				return false, err
			}
			for _, raw := range source.Actions {
				fields, err := assistantTestObject(raw)
				if err != nil {
					return false, err
				}
				if string(fields["id"]) == string(assistantChatJSON(in.DialectAction.ID)) {
					return false, errAssistantRunConflict
				}
			}
			op.Intent, _ = json.Marshal(in)
		}
		if name == "propose_dialect_recipe" {
			var source struct {
				Drafts []json.RawMessage `json:"inputDrafts"`
			}
			if json.Unmarshal(e.source.Original, &source) != nil {
				return false, errAssistantRunInvalid
			}
			if err := prepareAssistantDialectRecipeIntent(*run, &in, len(source.Drafts)); err != nil {
				return false, err
			}
			op.Intent, _ = json.Marshal(in)
		}
		return appendAssistantRunIntent(run, op)
	})
	e.changed(err)
	if err != nil {
		return refuse(err)
	}
	if replay != nil {
		value, err := runcontext.ParseJSON(replay.Result)
		if err != nil {
			return refuse(err)
		}
		if name == "propose_test_cases" {
			if err := e.service.server.materializeAssistantTests(e.request.WithContext(ctx), e.record.ID); err != nil {
				return refuse(err)
			}
		}
		return runcontext.ReadToolResult(value), nil
	}
	if err = e.Dispatch(ctx, id); err != nil {
		return refuse(err)
	}
	// Proposals wait for the existing project exclusion without a private-data
	// lock, then read current custody and retain the result in one bounded span.
	var unlock func()
	var lockErr error
	if name == "propose_graph" {
		unlock, lockErr = e.service.server.lockProject(ctx, projectLockWait)
		if unlock != nil {
			defer unlock()
		}
	}
	var result runcontext.ToolResult
	var read *assistantGraphRead
	var testsRead *assistantTestRead
	var dialectRead *assistantDialectRead
	var output *assistantDomainOutput
	workErr := lockErr
	if workErr == nil {
		if in.RecipeProposal != nil {
			ctx = context.WithValue(ctx, assistantDialectRecipePreparedKey{}, in.RecipeProposal)
		}
		if in.DialectAction != nil {
			ctx = context.WithValue(ctx, assistantDialectPreparedKey{}, in.DialectAction)
		}
		var observation assistantHostObservation
		observation, workErr = work(ctx, root)
		result, read, testsRead, dialectRead, output = observation.Result, observation.GraphRead, observation.TestsRead, observation.DialectRead, observation.Output
	}
	if workErr == nil && output != nil && output.Kind == "graph" && !result.Err() && testHookAssistantGraphObserved != nil {
		testHookAssistantGraphObserved(result.Clone(), output.Draft)
	}
	if workErr == nil && output != nil && assistantPolicyTool(name) && testHookAssistantPolicyObserved != nil {
		testHookAssistantPolicyObserved()
	}
	if workErr == nil && output != nil && output.Kind == "test-proposal" && testHookAssistantTestsObserved != nil {
		testHookAssistantTestsObserved()
	}
	state := "completed"
	if workErr != nil {
		dialectRead = nil
		testsRead = nil
		result = assistantHostFailure(workErr)
		if assistantDialectRecipeTool(name) {
			result = assistantDialectRecipeFailure(workErr)
		}
		if assistantDialectTool(name) {
			result = assistantDialectFailure(workErr)
		}
		read = nil
		output = nil
		state = "refused"
	}
	if result.Err() {
		dialectRead = nil
		testsRead = nil
		state = "refused"
		read = nil
		dialectDiagnostic := output != nil && output.Kind == "dialect-action" && output.DialectAction != nil && output.DialectAction.Action != nil && output.DialectAction.Action.Kind == "update-dialect" && !output.DialectAction.Ready
		if !dialectDiagnostic && (output == nil || output.Kind != "policy-update" || output.PolicyUpdate == nil || output.PolicyUpdate.Check == nil || output.PolicyUpdate.Check.Status != "stale") {
			output = nil
		}
	}
	retained := assistantHostResult{Result: result.Value().JSON(), Read: read, Tests: testsRead, DialectRead: dialectRead}
	if output != nil {
		retained.PolicySource = output.PolicySource
	}
	receipt, _ := json.Marshal(retained)
	// Only the observed result/output outlives cancellation. Authorization,
	// dispatch, lock acquisition and producer work above keep the caller context.
	observed, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finish()
	err = e.writer.change(observed, true, func(run *assistantRunRecord) (bool, error) {
		for i := range run.Operations {
			held := &run.Operations[i]
			if held.ID != id {
				continue
			}
			if held.State != "dispatched" && held.State != "uncertain" {
				return false, errAssistantRunConflict
			}
			if output != nil {
				output.RunID, output.RequestKey, output.OperationID, output.CallID = run.ID, run.Binding.ExecutionKey, id, in.CallID
				run.DomainOutputs = append(run.DomainOutputs, *output)
			}
			held.State = state
			held.Result = receipt
			return true, nil
		}
		return false, errAssistantRunInvalid
	})
	e.changed(err)
	if err != nil {
		return refuse(err)
	}
	if name == "propose_test_cases" && !result.Err() {
		if err := e.service.server.materializeAssistantTests(e.request.WithContext(observed), e.record.ID); err != nil {
			return refuse(err)
		}
	}
	var dialectUnknown *assistantDialectUnknown
	if (name == "propose_dialect" || name == "propose_dialect_publication") && errors.As(workErr, &dialectUnknown) {
		e.cancel()
	}
	var unknown *assistantDialectRecipeUnknown
	if name == "propose_dialect_recipe" && errors.As(workErr, &unknown) {
		// Stop only after preserving the exact failure and deterministic
		// metadata. Existing read/history/cancel APIs remain available.
		e.cancel()
	}
	return result, nil
}
