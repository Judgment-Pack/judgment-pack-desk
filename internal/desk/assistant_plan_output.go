package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type assistantPlanMetadata struct {
	ID        string                 `json:"id"`
	Revision  int                    `json:"revision"`
	CreatedAt string                 `json:"createdAt"`
	Bindings  []assistantPlanBinding `json:"bindings,omitempty"`
}

// Locators stay in private host custody. They never become opaque artifact IDs
// by encoding a path, and a handle grants no read or execution authority.
type assistantPlanBinding struct {
	ID      string                 `json:"id"`
	Kind    string                 `json:"kind"`
	Locator assistantPlanReference `json:"locator"`
}

func (m *assistantPlanMetadata) UnmarshalJSON(raw []byte) error {
	type plain assistantPlanMetadata
	if err := readAssistantDialectCustody(raw, reflect.TypeOf(plain{})); err != nil {
		return err
	}
	var value plain
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*m = assistantPlanMetadata(value)
	return nil
}

func assistantPlanHandle(binding assistantRunBinding, plan assistantPlanMetadata, locator assistantPlanReference, ordinal int) string {
	raw, _ := json.Marshal([]string{binding.Project, binding.Owner.Kind, binding.Owner.ID, binding.ExecutionKey, plan.ID, strconv.Itoa(plan.Revision), strconv.Itoa(ordinal), locator.Kind, locator.ID})
	return "plan-ref-" + digestOf(raw)
}

func validAssistantPlanBindings(run assistantRunRecord, meta *assistantPlanMetadata) bool {
	seen := map[string]bool{}
	locators := map[assistantPlanReference]bool{}
	for i, binding := range meta.Bindings {
		kind := "artifact"
		if binding.Locator.Kind == "dialect" {
			kind = "dialect"
		}
		if len(meta.Bindings) > 8 || !validAssistantPlanReference(binding.Locator) || binding.Kind != kind || binding.ID != assistantPlanHandle(run.Binding, *meta, binding.Locator, i) || seen[binding.ID] || locators[binding.Locator] {
			return false
		}
		seen[binding.ID], locators[binding.Locator] = true, true
	}
	return true
}

func assistantPlanID(binding assistantRunBinding, in assistantHostIntent) string {
	return "plan-" + digestOf([]byte(binding.ExecutionKey + "\x00" + in.CallID + "\x00" + in.CallbackID))[:40]
}
func assistantPlanOperationID(in assistantHostIntent) string {
	in.Plan = nil
	raw, _ := json.Marshal(in)
	return "plan-host-" + digestOf(raw)[:32]
}
func prepareAssistantPlanIntent(run assistantRunRecord, source []byte, in *assistantHostIntent) error {
	if !in.Root || run.Binding.Phase != "plan" {
		return errAssistantRunScope
	}
	a, err := readAssistantPlanArguments(in.Arguments)
	if err != nil {
		return err
	}
	plans, err := assistantPlanSource(source)
	if err != nil {
		return err
	}
	n := len(plans)
	for _, out := range run.DomainOutputs {
		if out.Kind == "plan" {
			n++
		}
	}
	if n >= 24 {
		return errAssistantRunInvalid
	}
	in.Plan = &assistantPlanMetadata{ID: assistantPlanID(run.Binding, *in), Revision: n + 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	refs, err := assistantPlanReferences(source, a.References)
	if err != nil {
		return err
	}
	for i, ref := range refs {
		kind := "artifact"
		if ref.Kind == "dialect" {
			kind = "dialect"
		}
		in.Plan.Bindings = append(in.Plan.Bindings, assistantPlanBinding{ID: assistantPlanHandle(run.Binding, *in.Plan, ref, i), Kind: kind, Locator: ref})
	}
	return nil
}
func validateAssistantPlanIntent(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent) error {
	if !in.Root {
		return errAssistantRunInvalid
	}
	if in.Plan == nil || run.Binding.Phase != "plan" || in.Plan.ID != assistantPlanID(run.Binding, in) || in.Plan.Revision < 1 || in.Plan.Revision > 24 || op.ID != assistantPlanOperationID(in) || in.Policy != nil || in.RecipeProposal != nil || in.DialectAction != nil {
		return errAssistantRunInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, in.Plan.CreatedAt); err != nil {
		return errAssistantRunInvalid
	}
	if !validAssistantPlanBindings(run, in.Plan) {
		return errAssistantRunInvalid
	}
	_, err := readAssistantPlanArguments(in.Arguments)
	return err
}
func assistantPlanResult(plan chatPlan) (runcontext.ToolResult, error) {
	result := map[string]any{"status": "review-required", "revision": plan.Revision, "message": "Plan saved in this chat. The person must choose Start work to execute it. No planned operations have started."}
	for _, ref := range plan.References {
		if ref.Revision == "" {
			result["planId"], result["references"] = plan.ID, plan.References
			break
		}
	}
	return assistantHostJSON(result)
}
func validateAssistantPlanOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	if out.Plan == nil || out.Kind != "plan" || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey {
		return errAssistantRunInvalid
	}
	base := out
	base.Plan = nil
	if !reflect.DeepEqual(base, assistantDomainOutput{Kind: "plan", RunID: out.RunID, RequestKey: out.RequestKey, OperationID: out.OperationID, CallID: out.CallID}) {
		return errAssistantRunInvalid
	}
	op := assistantDialectOperation(run, out.OperationID)
	var in assistantHostIntent
	if op == nil || op.Kind != "host" || op.Name != assistantPlanTool || op.State != "completed" || readAssistantHostReceipt(op.Intent, &in) != nil || validateAssistantPlanIntent(run, *op, in) != nil || in.CallID != out.CallID {
		return errAssistantRunInvalid
	}
	a, err := readAssistantPlanArguments(in.Arguments)
	p := out.Plan
	if err != nil || p.ID != in.Plan.ID || p.Revision != in.Plan.Revision || p.CreatedAt != in.Plan.CreatedAt || p.Objective != a.Objective || !reflect.DeepEqual(p.Steps, a.Steps) || !reflect.DeepEqual(p.Questions, a.Questions) || p.Status != "proposed" || p.AcceptedAt != "" || !validAssistantPlanReferences(p.References) {
		return errAssistantRunInvalid
	}
	if len(in.Plan.Bindings) > 0 {
		if len(in.Plan.Bindings) != len(p.References) {
			return errAssistantRunInvalid
		}
		for i, binding := range in.Plan.Bindings {
			ref := p.References[i]
			if ref.Kind != binding.Kind || ref.ID != binding.ID || ref.Revision != "" || !assistantRunDigest(ref.Digest) {
				return errAssistantRunInvalid
			}
		}
	} else {
		for _, ref := range p.References {
			if ref.Revision == "" {
				return errAssistantRunInvalid
			}
		}
	}
	for _, ref := range a.References {
		found := false
		if len(in.Plan.Bindings) > 0 {
			for _, held := range in.Plan.Bindings {
				found = found || held.Locator == ref
			}
		} else {
			for _, held := range p.References {
				found = found || held.Kind == ref.Kind && held.ID == ref.ID && held.Revision != ""
			}
		}
		if !found {
			return errAssistantRunInvalid
		}
	}
	expected, err := assistantPlanResult(*p)
	receipt, ok := receipts[op.ID]
	if err != nil || !ok || receipt.Read != nil || receipt.Tests != nil || receipt.DialectRead != nil || receipt.PolicySource != nil || !bytes.Equal(expected.Value().JSON(), receipt.Result) {
		return errAssistantRunInvalid
	}
	return nil
}
func validAssistantPlanReferences(refs []chatPlanReference) bool {
	if refs == nil || len(refs) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		key := ref.Kind + "\x00" + ref.ID
		if ref.Revision == "" {
			key = "current\x00" + ref.ID
		}
		if !validChatPlanReference(ref) || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}
func assistantPlanSource(source []byte) ([]chatPlan, error) {
	if _, err := assistantChatValue(source); err != nil {
		return nil, err
	}
	if err := validateChatPlan(source); err != nil {
		return nil, err
	}
	var chat struct {
		Interaction json.RawMessage `json:"interaction"`
		Plans       []chatPlan      `json:"plans"`
	}
	if json.Unmarshal(source, &chat) != nil {
		return nil, errAssistantRunInvalid
	}
	if _, err := assistantChatPhase(chat.Interaction); err != nil {
		return nil, err
	}
	return chat.Plans, nil
}

func appendAssistantPlanOutputs(chat map[string]json.RawMessage, outputs []assistantDomainOutput) error {
	for _, out := range outputs {
		if out.Kind != "plan" {
			continue
		}
		plans, err := assistantPlanSource(assistantChatEncodeObject(chat))
		if err != nil || out.Plan == nil {
			return errAssistantRunInvalid
		}
		phase, err := assistantChatPhase(chat["interaction"])
		if err != nil || phase != "plan" {
			return errAssistantRunScope
		}
		if len(plans) >= 24 || out.Plan.Revision != len(plans)+1 {
			return errAssistantRunConflict
		}
		for i := range plans {
			if plans[i].ID == out.Plan.ID {
				return errAssistantRunConflict
			}
			plans[i].Status = "superseded"
		}
		plans = append(plans, *out.Plan)
		chat["plans"] = assistantChatJSON(plans)
	}
	return nil
}
