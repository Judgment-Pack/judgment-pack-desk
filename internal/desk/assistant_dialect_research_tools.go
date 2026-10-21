package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

const assistantResearchTool = "request_dialect_research"
const assistantResearchOperation = "dialect-research/request"

func readAssistantDialectResearchArguments(raw []byte) (assistantDialectArguments, error) {
	var a assistantDialectArguments
	fields, err := assistantTestControls(raw, "id", "revision")
	if len(raw) > 4096 || !validUTF8(raw) || readAssistantDialectCustody(raw, reflect.TypeOf(struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
	}{})) != nil || err != nil || len(fields) != 2 || json.Unmarshal(raw, &a) != nil || !inputPreparationID.MatchString(a.ID) || !assistantRunDigest(a.Revision) {
		return a, failure.ToolInput("Use only the saved Dialect id and exact revision from a current root recipe read. No allowance was created.")
	}
	return a, nil
}
func (e *assistantRunExecution) assistantDialectResearchTools() (*assistantHostRegistry, error) {
	schema := assistantChatJSON(map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "pattern": inputPreparationID.String()}, "revision": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}}, "required": []string{"id", "revision"}, "additionalProperties": false})
	return newAssistantHostRegistry([]runcontext.HostTool{{Name: assistantResearchTool, Effect: "workflow", Description: "Request research using only an existing exact user-reviewed research allowance for this chat, session, turn and saved recipe. Read the recipe first. Without an allowance retain a review card. Started means queued, not completed. This never grants permission, publishes, changes limits or repeats an uncertain request.", InputSchema: schema, Execute: func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
		a, err := readAssistantDialectResearchArguments(raw)
		if err != nil {
			return assistantDialectFailure(err), nil
		}
		return e.invokeObservedHost(ctx, assistantResearchTool, raw, func(ctx context.Context, root bool) (assistantHostObservation, error) {
			if !root || e.record.Binding.Phase != "work" {
				return assistantHostObservation{}, errAssistantRunScope
			}
			return e.executeAssistantResearchRequest(ctx, a)
		})
	}}})
}

// The owner already holds the private lock. Resolve the full original binding,
// including Policy/profile/source, without calling Authorize (which reacquires it).
func (e *assistantRunExecution) authorizeAssistantResearchLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	s := e.service.server
	identity, _, err := s.assistantRunIdentity(e.request.WithContext(ctx))
	if err != nil {
		return err
	}
	fresh, err := s.resolveAssistantRun(e.request.WithContext(ctx), e.source.Start, identity, e.record.Binding.Project)
	if err != nil {
		return err
	}
	fresh.Binding.ExecutionKey = e.source.Binding.ExecutionKey
	fresh.Binding.Unit = e.source.Binding.Unit
	if !assistantRunFreshSourceEqual(e.source, fresh) {
		return errAssistantRunScope
	}
	if e.input == nil {
		return errAssistantRunUnavailable
	}
	e.input.mu.Lock()
	held := e.input.critic
	e.input.mu.Unlock()
	if held != nil {
		current, err := e.readAssistantCriticSnapshotOwned(ctx, true)
		if err != nil {
			return err
		}
		if !assistantCriticSame(held, current) {
			return errAssistantRunScope
		}
	}
	return nil
}
func assistantResearchRequestKey(run assistantRunRecord, host string) string {
	return digestOf([]byte(run.Binding.ExecutionKey + "\x00" + host))
}

type assistantResearchIntent struct {
	HostOperationID string               `json:"hostOperationId"`
	ResearchRequest inputResearchRequest `json:"researchRequest"`
}
type assistantResearchDecision struct {
	Receipt *inputResearchRequestReceipt `json:"receipt,omitempty"`
	Refused bool                         `json:"refused"`
}
type assistantDialectResearchOutput struct {
	OperationID string                      `json:"operationId"`
	Receipt     inputResearchRequestReceipt `json:"receipt"`
	Recovered   bool                        `json:"recovered"`
}

func (e *assistantRunExecution) executeAssistantResearchRequest(ctx context.Context, a assistantDialectArguments) (assistantHostObservation, error) {
	empty := assistantHostObservation{}
	meta, ok := ctx.Value(assistantDialectPreparedKey{}).(*assistantDialectActionMetadata)
	if !ok || meta == nil {
		return empty, errAssistantRunScope
	}
	record, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return empty, err
	}
	readOp := assistantDialectOperation(record, meta.ReadOperationID)
	var readReceipt assistantHostResult
	if readOp == nil || readAssistantHostReceipt(readOp.Result, &readReceipt) != nil || readReceipt.DialectRead == nil || !validAssistantDialectActionRead(*readReceipt.DialectRead) || readReceipt.DialectRead.Recipe.Recipe.Research == nil {
		return empty, errAssistantRunScope
	}
	read := *readReceipt.DialectRead
	host := ""
	for _, op := range record.Operations {
		var in assistantHostIntent
		if op.Kind == "host" && op.Name == assistantResearchTool && readAssistantHostReceipt(op.Intent, &in) == nil && reflect.DeepEqual(in.DialectAction, meta) {
			host = op.ID
		}
	}
	if host == "" || a.ID != read.ID || a.Revision != read.Revision {
		return empty, errAssistantRunScope
	}
	// Intent precedes admission. Its exact request is the only permitted recovery key.
	request := inputResearchRequest{ChatID: record.Binding.Owner.ID, PreparationID: a.ID, Revision: a.Revision, TurnID: record.Binding.RequestID, RequestKey: assistantResearchRequestKey(record, host)}
	op, err := e.boundIntent(ctx, "action", assistantResearchOperation, func(string) ([]byte, error) { return json.Marshal(assistantResearchIntent{host, request}) })
	if err != nil {
		return empty, err
	}
	if err = e.recheckAssistantCritic(ctx); err != nil {
		return empty, err
	}
	if err = e.Dispatch(ctx, op.ID); err != nil {
		return empty, err
	}
	receipt, requestErr := e.service.server.requestResearch(e.request.WithContext(ctx), request, e.authorizeAssistantResearchLocked)
	// An admitted same-turn receipt is historical. A new unadmitted review card
	// must still match its current explicit recipe read at producer completion.
	if requestErr == nil && receipt.Status == "review-required" {
		requestErr = e.recheckAssistantDialectAction(ctx, read)
	}
	if requestErr != nil {
		var unknown *inputResearchRequestUnknown
		if errors.As(requestErr, &unknown) {
			return empty, requestErr
		}
		if err = e.Result(ctx, op.ID, "refused", assistantChatJSON(assistantResearchDecision{Refused: true})); err != nil {
			return empty, &inputResearchRequestUnknown{err}
		}
		return empty, requestErr
	}
	if err = e.Result(ctx, op.ID, "completed", assistantChatJSON(assistantResearchDecision{Receipt: &receipt})); err != nil {
		return empty, &inputResearchRequestUnknown{err}
	}
	out := assistantResearchOutput(*meta, read, op.ID, receipt, false)
	result, err := assistantResearchResult(out)
	return assistantHostObservation{Result: result, Output: &assistantDomainOutput{Kind: "dialect-action", DialectAction: out}}, err
}
func assistantResearchOutput(meta assistantDialectActionMetadata, read assistantDialectRead, op string, receipt inputResearchRequestReceipt, recovered bool) *assistantDialectActionOutput {
	a := newAssistantDialectAction(meta, read, "research")
	if receipt.Status == "started" {
		a.Applied = &assistantDialectActionApplied{At: receipt.UsedAt, RunID: receipt.RunID}
	}
	return &assistantDialectActionOutput{Read: read, Action: a, Ready: true, Research: &assistantDialectResearchOutput{OperationID: op, Receipt: receipt, Recovered: recovered}}
}
func assistantResearchResult(out *assistantDialectActionOutput) (runcontext.ToolResult, error) {
	if out.Action == nil || out.Research == nil {
		return runcontext.ToolResult{}, errAssistantRunInvalid
	}
	receipt := out.Research.Receipt
	value := map[string]any{"status": receipt.Status, "actionId": out.Action.ID, "inputId": out.Action.InputID, "kind": "research"}
	if receipt.Status == "started" {
		value["runId"] = receipt.RunID
		value["message"] = "The existing reviewed allowance admitted this research run. Queued is not completed; the independent worker is not cancelled by chat Stop."
	} else {
		value["message"] = "Review research in chat. No matching unused allowance was consumed and no research was started."
	}
	return assistantHostJSON(value)
}
func sameResearchReceipt(a, b inputResearchRequestReceipt) bool {
	// Worker state may advance independently; admission identity never changes.
	a.State = ""
	b.State = ""
	return reflect.DeepEqual(a, b)
}
func validResearchRequestReceipt(r inputResearchRequestReceipt) bool {
	if r.Status == "review-required" {
		return r.RunID == "" && r.UsedAt == "" && r.RequestKey == "" && r.State == ""
	}
	if r.Status != "started" || !inputResearchID.MatchString(r.RunID) || !assistantRunText(r.State, 64) || r.RequestKey != "" && !assistantRunDigest(r.RequestKey) {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, r.UsedAt)
	return err == nil
}

// Import validates all finite request/receipt associations. Physical owner readback
// remains necessary for projection; an imported browser card grants nothing.
func validateAssistantResearchOperations(run assistantRunRecord) error {
	seen := map[string]bool{}
	for _, op := range run.Operations {
		if op.Name != assistantResearchOperation {
			if assistantDialectReserved(op.Intent, "researchRequest") {
				return errAssistantRunInvalid
			}
			continue
		}
		var in assistantResearchIntent
		if op.Kind != "action" || readAssistantDialectCustody(op.Intent, reflect.TypeOf(in)) != nil || readAssistantHostReceipt(op.Intent, &in) != nil {
			return errAssistantRunInvalid
		}
		host := assistantDialectOperation(run, in.HostOperationID)
		var h assistantHostIntent
		if host == nil || host.Kind != "host" || host.Name != assistantResearchTool || !assistantDialectAfter(run, op.ID, host.ID) || readAssistantHostReceipt(host.Intent, &h) != nil || h.DialectAction == nil || !h.Root || run.Binding.Phase != "work" || seen[host.ID] {
			return errAssistantRunInvalid
		}
		seen[host.ID] = true
		a, err := readAssistantDialectResearchArguments(h.Arguments)
		q := in.ResearchRequest
		if err != nil || !validResearchRequest(q) || q.ChatID != run.Binding.Owner.ID || q.TurnID != run.Binding.RequestID || q.PreparationID != a.ID || q.Revision != a.Revision || q.RequestKey != assistantResearchRequestKey(run, host.ID) {
			return errAssistantRunInvalid
		}
		if op.Result != nil {
			var result assistantResearchDecision
			if readAssistantHostReceipt(op.Result, &result) != nil || (op.State == "refused") != result.Refused || result.Refused != (result.Receipt == nil) || result.Receipt != nil && !validResearchRequestReceipt(*result.Receipt) {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}
func validateAssistantResearchOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult, host assistantRunOperation, in assistantHostIntent) error {
	p := out.DialectAction
	if p.Research == nil || !p.Ready || !validAssistantDialectActionRead(p.Read) || p.Read.Recipe.Recipe.Research == nil || p.CheckID != "" || p.MappingCheckID != "" || p.ActiveResearch != nil || p.Published != nil || p.Preview != nil {
		return errAssistantRunInvalid
	}
	op := assistantDialectOperation(run, p.Research.OperationID)
	var intent assistantResearchIntent
	var decision assistantResearchDecision
	if op == nil || op.Name != assistantResearchOperation || op.State != "completed" || readAssistantHostReceipt(op.Intent, &intent) != nil || intent.HostOperationID != host.ID || readAssistantHostReceipt(op.Result, &decision) != nil || decision.Receipt == nil || !reflect.DeepEqual(*decision.Receipt, p.Research.Receipt) {
		return errAssistantRunInvalid
	}
	expected := assistantResearchOutput(*in.DialectAction, p.Read, op.ID, *decision.Receipt, p.Research.Recovered)
	if !reflect.DeepEqual(p, expected) {
		return errAssistantRunInvalid
	}
	receipt, ok := receipts[host.ID]
	if !ok {
		return errAssistantRunInvalid
	}
	var result runcontext.ToolResult
	if p.Research.Recovered {
		if host.State != "refused" || decision.Receipt.Status != "started" {
			return errAssistantRunInvalid
		}
		result = assistantDialectFailure(&inputResearchRequestUnknown{})
	} else {
		if host.State != "completed" {
			return errAssistantRunInvalid
		}
		result, _ = assistantResearchResult(p)
	}
	if receipt.DialectRead != nil || receipt.Tests != nil || receipt.Read != nil || receipt.PolicySource != nil || !bytes.Equal(receipt.Result, result.Value().JSON()) {
		return errAssistantRunInvalid
	}
	return nil
}
