package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

type assistantDialectRecipeAttribution struct {
	Connection string `json:"connection"`
	Model      string `json:"model"`
}
type assistantDialectRecipeProposal struct {
	Version     int                                `json:"version"`
	DraftID     string                             `json:"draftId"`
	CreatedAt   string                             `json:"createdAt"`
	Name        string                             `json:"name"`
	Intent      string                             `json:"intent"`
	Target      string                             `json:"target"`
	Fields      []inputResearchField               `json:"fields"`
	Criteria    []inputResearchCriterion           `json:"criteria"`
	Limits      inputResearchLimits                `json:"limits"`
	Execution   *assistantDialectRecipeExecution   `json:"execution,omitempty"`
	AfterTurnID string                             `json:"afterTurnId"`
	Attribution *assistantDialectRecipeAttribution `json:"attribution"`
}

type assistantDialectRecipeRetention struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}
type assistantDialectRecipeOutput struct {
	Proposal assistantDialectRecipeProposal  `json:"proposal"`
	Retained assistantDialectRecipeRetention `json:"retained"`
}
type assistantDialectRecipePreparedKey struct{}

func (p assistantDialectRecipeProposal) content() assistantDialectRecipeContent {
	return assistantDialectRecipeContent{p.Name, p.Intent, p.Target, p.Fields, p.Criteria, p.Limits, p.Execution}
}
func assistantDialectRecipeProposalID(binding assistantRunBinding, in assistantHostIntent) string {
	return "recipe-" + digestOf([]byte(binding.ExecutionKey + "\x00" + in.CallID + "\x00" + in.CallbackID))[:32]
}
func assistantDialectRecipeOperationID(in assistantHostIntent) string {
	in.RecipeProposal = nil
	raw, _ := json.Marshal(in)
	return "dialect-recipe-host-" + digestOf(raw)[:32]
}
func validAssistantDialectRecipeProposal(binding assistantRunBinding, in assistantHostIntent, p assistantDialectRecipeProposal) bool {
	args, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", in.Arguments)
	_, clockErr := time.Parse(time.RFC3339Nano, p.CreatedAt)
	return err == nil && clockErr == nil && p.Version == 1 && p.DraftID == assistantDialectRecipeProposalID(binding, in) && p.AfterTurnID == binding.RequestID && assistantDialectRecipeText(p.AfterTurnID, 160) && p.Attribution != nil && assistantDialectRecipeText(p.Attribution.Connection, 128) && assistantDialectRecipeText(p.Attribution.Model, 128) && p.Attribution.Connection == binding.ConnectionID && p.Attribution.Model == binding.Model && reflect.DeepEqual(p.content(), args.Content)
}
func prepareAssistantDialectRecipeIntent(run assistantRunRecord, in *assistantHostIntent, sourceCount int) error {
	args, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", in.Arguments)
	if err != nil {
		return err
	}
	count := 0
	// Source capacity is checked separately in the live service; every
	// retained intent counts here, including uncertain work.
	for _, held := range run.Operations {
		if held.Kind == "host" && held.Name == "propose_dialect_recipe" {
			count++
		}
	}
	if sourceCount < 0 || sourceCount+count >= 16 {
		return errAssistantRunInvalid
	}
	a := args.Content
	in.RecipeProposal = &assistantDialectRecipeProposal{Version: 1, DraftID: assistantDialectRecipeProposalID(run.Binding, *in), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Name: a.Name, Intent: a.Intent, Target: "records", Fields: a.Fields, Criteria: a.Criteria, Limits: a.Limits, Execution: a.Execution, AfterTurnID: run.Binding.RequestID, Attribution: &assistantDialectRecipeAttribution{run.Binding.ConnectionID, run.Binding.Model}}
	return nil
}
func validateAssistantDialectRecipeIntent(run assistantRunRecord, op assistantRunOperation, in assistantHostIntent) error {
	if in.Policy != nil {
		return errAssistantRunInvalid
	}
	if op.Name != "propose_dialect_recipe" {
		if in.RecipeProposal != nil {
			return errAssistantRunInvalid
		}
		return nil
	}
	if !in.Root || run.Binding.Phase != "work" || in.Policy != nil || in.RecipeProposal == nil || op.ID != assistantDialectRecipeOperationID(in) || op.CallID != op.ID || !validAssistantDialectRecipeProposal(run.Binding, in, *in.RecipeProposal) {
		return errAssistantRunInvalid
	}
	// Validate array/nested proposal members before decoding could discard extras.
	members, err := configMembers(op.Intent)
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.name != "recipeProposal" {
			continue
		}
		raw := op.Intent[member.start:member.end]
		fields, err := assistantChatObject(raw)
		if err != nil {
			return err
		}
		content := map[string]json.RawMessage{}
		for _, key := range []string{"name", "intent", "target", "fields", "criteria", "limits", "execution"} {
			if v := fields[key]; v != nil {
				content[key] = v
			}
		}
		if _, err := readAssistantDialectRecipeArguments("propose_dialect_recipe", assistantChatEncodeObject(content)); err != nil {
			return err
		}
	}
	return nil
}
func assistantDialectRecipeResult(p assistantDialectRecipeProposal, draftID string) (runcontext.ToolResult, error) {
	return assistantHostJSON(struct {
		ArtifactType string `json:"artifactType"`
		Status       string `json:"status"`
		DraftID      string `json:"draftId"`
		ReviewURL    string `json:"reviewUrl"`
		Message      string `json:"message"`
	}{"input-recipe", "proposed", p.DraftID, "/dialects/drafts/" + draftID, "Dialect draft retained independently of chat. Review recipe opens its page. The person must finish setup and use Save to Dialects. Research has not started."})
}
func validAssistantDialectRecipeRetained(binding assistantRunBinding, p assistantDialectRecipeProposal, saved dialectDraftReply) bool {
	d := saved.Document
	expectedID := dialectDraftIdentity("", &dialectDraftOrigin{binding.Owner.ID, p.DraftID})
	if !assistantRunDigest(saved.SHA256) || d.Version != 1 || d.ID != expectedID || d.Origin == nil || d.Origin.ChatID != binding.Owner.ID || d.Origin.DraftID != p.DraftID || d.Name != p.Name || !sameDraftValue(d.Recipe, p.content().recipe()) || d.CreatedAt != p.CreatedAt || d.UpdatedAt != p.CreatedAt || d.Discarded || d.SavedInputID != "" {
		return false
	}
	_, snapshot, err := encodeDialectDraft(d)
	return err == nil && snapshot.SHA256 == saved.SHA256
}
func assistantDialectRecipeDocument(binding assistantRunBinding, p assistantDialectRecipeProposal) dialectDraftDocument {
	origin := &dialectDraftOrigin{binding.Owner.ID, p.DraftID}
	return dialectDraftDocument{Version: 1, ID: dialectDraftIdentity("", origin), Name: p.Name, Recipe: p.content().recipe(), Origin: origin, CreatedAt: p.CreatedAt, UpdatedAt: p.CreatedAt}
}
func validAssistantDialectRecipeRetention(binding assistantRunBinding, p assistantDialectRecipeProposal, saved assistantDialectRecipeRetention) bool {
	doc := assistantDialectRecipeDocument(binding, p)
	_, snapshot, err := encodeDialectDraft(doc)
	return err == nil && saved.ID == doc.ID && snapshot.SHA256 == saved.SHA256
}
func validateAssistantDialectRecipeOutput(run assistantRunRecord, out assistantDomainOutput, receipts map[string]assistantHostResult) error {
	if out.Kind != "dialect-recipe" || out.Recipe == nil || out.TestProposal != nil || out.PolicyUpdate != nil || out.PolicyInterpretation != nil || out.PolicySource != nil || !reflect.DeepEqual(out.Draft, graphDraft{}) || out.RunID != run.ID || out.RequestKey != run.Binding.ExecutionKey {
		return errAssistantRunInvalid
	}
	for _, op := range run.Operations {
		if op.ID != out.OperationID {
			continue
		}
		var in assistantHostIntent
		if op.Kind != "host" || op.Name != "propose_dialect_recipe" || op.State != "completed" || readAssistantHostReceipt(op.Intent, &in) != nil || validateAssistantDialectRecipeIntent(run, op, in) != nil || in.CallID != out.CallID || !reflect.DeepEqual(in.RecipeProposal, &out.Recipe.Proposal) || !validAssistantDialectRecipeRetention(run.Binding, out.Recipe.Proposal, out.Recipe.Retained) {
			return errAssistantRunInvalid
		}
		held, ok := receipts[op.ID]
		if !ok || held.Tests != nil || held.Read != nil || held.PolicySource != nil {
			return errAssistantRunInvalid
		}
		expected, err := assistantDialectRecipeResult(out.Recipe.Proposal, out.Recipe.Retained.ID)
		if err != nil || !bytes.Equal(expected.Value().JSON(), held.Result) {
			return errAssistantRunInvalid
		}
		return nil
	}
	return errAssistantRunInvalid
}
func appendAssistantDialectRecipeOutputs(chat map[string]json.RawMessage, outputs []assistantDomainOutput) error {
	selected := []assistantDomainOutput{}
	for _, out := range outputs {
		if out.Kind == "dialect-recipe" {
			selected = append(selected, out)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	existing := chat["inputDrafts"]
	if existing == nil {
		existing = []byte("[]")
	}
	list, err := assistantChatArray(existing)
	if err != nil || len(list)+len(selected) > 16 {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, raw := range list {
		var p struct {
			DraftID string `json:"draftId"`
		}
		if json.Unmarshal(raw, &p) != nil || p.DraftID == "" || seen[p.DraftID] {
			return errAssistantRunInvalid
		}
		seen[p.DraftID] = true
	}
	for _, out := range selected {
		p := out.Recipe.Proposal
		if seen[p.DraftID] {
			return errAssistantRunConflict
		}
		seen[p.DraftID] = true
		list = append(list, assistantChatJSON(p))
	}
	chat["inputDrafts"] = assistantChatEncodeArray(list)
	return nil
}

func assistantDialectRecipeMetadata(raw []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for key := range fields {
		if strings.EqualFold(key, "recipeProposal") {
			return true
		}
	}
	return false
}
