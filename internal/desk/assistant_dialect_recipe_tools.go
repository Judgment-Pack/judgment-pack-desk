package desk

// The four recipe tools expose portable design data only. The independent draft
// service, never this tool's model fields, owns retention and later publication.
import (
	"context"
	"encoding/json"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/delegation"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

var assistantDialectRecipeNames = []string{"get_dialect_recipe_instructions", "list_dialects", "read_dialect_recipe", "propose_dialect_recipe"}

func assistantDialectRecipeTool(name string) bool { return contains(assistantDialectRecipeNames, name) }

type assistantDialectRecipeContent struct {
	Name      string                           `json:"name"`
	Intent    string                           `json:"intent"`
	Target    string                           `json:"target,omitempty"`
	Fields    []inputResearchField             `json:"fields"`
	Criteria  []inputResearchCriterion         `json:"criteria"`
	Limits    inputResearchLimits              `json:"limits"`
	Execution *assistantDialectRecipeExecution `json:"execution,omitempty"`
}
type assistantDialectRecipeExecution delegation.Policy

func (p assistantDialectRecipeExecution) Validate() error { return delegation.Policy(p).Validate() }
func (p *assistantDialectRecipeExecution) policy() *delegation.Policy {
	if p == nil {
		return nil
	}
	value := delegation.Policy(*p)
	return &value
}

type assistantDialectRecipeArguments struct {
	ID      string
	Content assistantDialectRecipeContent
}

func (a assistantDialectRecipeContent) recipe() inputRecipe {
	limits := a.Limits
	// Exactly the browser's recipeFromProposal defaults: connections/search and
	// source requests remain for human review, never implicit tool authority.
	limits.Searches = 0
	return inputRecipe{Intent: a.Intent, Research: &inputResearchRecipe{Version: 1, Target: "records", Fields: a.Fields, Criteria: a.Criteria, Limits: limits, Execution: a.Execution.policy(), Retries: &inputResearchRetryPolicy{Mode: "temporary", MaxRetries: 2}}}
}

// Match the existing browser content validator's ECMAScript trim set. This is
// a blankness check only; retained model strings are never normalized/trimmed.
const assistantDialectRecipeWhitespace = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

func assistantDialectRecipeText(value string, max int) bool {
	return strings.Trim(value, assistantDialectRecipeWhitespace) != "" && runcontext.U16Length(value) <= max
}
func validAssistantDialectRecipeContent(a assistantDialectRecipeContent) bool {
	if !assistantDialectRecipeText(a.Name, 160) || !assistantDialectRecipeText(a.Intent, 12000) || a.Target != "records" || a.Fields == nil || len(a.Fields) < 1 || len(a.Fields) > 30 || a.Criteria == nil || len(a.Criteria) > 30 {
		return false
	}
	seen := map[string]bool{}
	for _, f := range a.Fields {
		if !researchFieldName.MatchString(f.Name) || contains([]string{"constructor", "prototype", "__proto__"}, f.Name) || seen[f.Name] || !assistantDialectRecipeText(f.Description, 1000) || !contains(researchFieldTypes, f.Type) {
			return false
		}
		seen[f.Name] = true
	}
	seen = map[string]bool{}
	for _, c := range a.Criteria {
		if !researchFieldName.MatchString(c.ID) || seen[c.ID] || !assistantDialectRecipeText(c.Description, 2000) || runcontext.U16Length(c.QualificationTarget) > 512 {
			return false
		}
		seen[c.ID] = true
	}
	l := a.Limits
	return l.Records >= 1 && l.Records <= 50 && l.Searches >= 0 && l.Searches <= 30 && l.Pages >= 0 && l.Pages <= 100 && l.ModelCalls >= 1 && l.ModelCalls <= 60 && (a.Execution == nil || a.Execution.Validate() == nil) && validateDialectDraft(a.Name, a.recipe()) == nil
}
func readAssistantDialectRecipeArguments(name string, raw json.RawMessage) (assistantDialectRecipeArguments, error) {
	var a assistantDialectRecipeArguments
	bad := func() (assistantDialectRecipeArguments, error) {
		return a, failure.ToolInput("Use the declared Dialect recipe fields and bounded limits. No draft was saved and no research started.")
	}
	if len(raw) > dialectDraftLimit || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil {
		return bad()
	}
	if name == "get_dialect_recipe_instructions" || name == "list_dialects" {
		var empty struct{}
		if readAssistantHostReceipt(raw, &empty) != nil {
			return bad()
		}
		return a, nil
	}
	if name == "read_dialect_recipe" {
		var selected struct {
			ID string `json:"id"`
		}
		if readAssistantHostReceipt(raw, &selected) != nil || !inputPreparationID.MatchString(selected.ID) {
			return bad()
		}
		a.ID = selected.ID
		return a, nil
	}
	if name != "propose_dialect_recipe" || readAssistantHostReceipt(raw, &a.Content) != nil {
		return bad()
	}
	var wire struct {
		Fields   []json.RawMessage `json:"fields"`
		Criteria []json.RawMessage `json:"criteria"`
		Limits   json.RawMessage   `json:"limits"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return bad()
	}
	var limits inputResearchLimits
	if readAssistantHostReceipt(wire.Limits, &limits) != nil {
		return bad()
	}
	for _, raw := range wire.Fields {
		var field inputResearchField
		if readAssistantHostReceipt(raw, &field) != nil {
			return bad()
		}
	}
	for _, raw := range wire.Criteria {
		var criterion inputResearchCriterion
		if readAssistantHostReceipt(raw, &criterion) != nil {
			return bad()
		}
		members, _ := configMembers(raw)
		for _, member := range members {
			if member.name == "qualificationTarget" && !assistantDialectRecipeText(criterion.QualificationTarget, 512) {
				return bad()
			}
		}
	}
	members, _ := configMembers(raw)
	for _, member := range members {
		if member.name == "target" && a.Content.Target != "records" {
			return bad()
		}
	}
	a.Content.Target = "records"
	if !validAssistantDialectRecipeContent(a.Content) {
		return bad()
	}
	return a, nil
}
func assistantDialectRecipeSchemas() map[string]json.RawMessage {
	object := func(properties map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	text := func(n int) any { return map[string]any{"type": "string", "minLength": 1, "maxLength": n} }
	integer := func(lo, hi int) any { return map[string]any{"type": "integer", "minimum": lo, "maximum": hi} }
	field := object(map[string]any{"name": map[string]any{"type": "string", "pattern": researchFieldName.String()}, "description": text(1000), "type": map[string]any{"type": "string", "enum": researchFieldTypes}}, "name", "description", "type")
	criterion := object(map[string]any{"id": map[string]any{"type": "string", "pattern": researchFieldName.String()}, "description": text(2000), "qualificationTarget": text(512)}, "id", "description")
	proposal := object(map[string]any{"name": text(160), "intent": text(12000), "fields": map[string]any{"type": "array", "minItems": 1, "maxItems": 30, "items": field}, "criteria": map[string]any{"type": "array", "maxItems": 30, "items": criterion}, "limits": object(map[string]any{"records": integer(1, 50), "searches": integer(0, 30), "pages": integer(0, 100), "modelCalls": integer(1, 60)}, "records", "searches", "pages", "modelCalls"), "execution": object(map[string]any{"approach": map[string]any{"type": "string", "enum": []string{"standard", "delegated"}}, "concurrency": map[string]any{"type": "integer", "enum": []int{0, 1, 2}}}, "approach", "concurrency")}, "name", "intent", "fields", "criteria", "limits")
	schemas := map[string]json.RawMessage{}
	for _, name := range assistantDialectRecipeNames {
		value := object(map[string]any{})
		if name == "read_dialect_recipe" {
			value = object(map[string]any{"id": map[string]any{"type": "string", "pattern": inputPreparationID.String()}}, "id")
		}
		if name == "propose_dialect_recipe" {
			value = proposal
		}
		schemas[name], _ = json.Marshal(value)
	}
	return schemas
}

// Explicit safe projection of a saved recipe: no AI/search binding, retained
// record values, source bytes, credential or embedded pack enters this view.
type assistantDialectRecipeRead struct {
	ID            string                             `json:"id"`
	Name          string                             `json:"name"`
	Version       int                                `json:"version"`
	Revision      string                             `json:"revision"`
	Intent        string                             `json:"intent"`
	Template      *inputTemplateOrigin               `json:"template,omitempty"`
	Research      *assistantDialectResearchRead      `json:"research,omitempty"`
	Qualification *assistantDialectQualificationRead `json:"qualification,omitempty"`
}
type assistantDialectResearchRead struct {
	Fields    []inputResearchField     `json:"fields"`
	Criteria  []inputResearchCriterion `json:"criteria"`
	Limits    inputResearchLimits      `json:"limits"`
	Execution *delegation.Policy       `json:"execution,omitempty"`
}
type assistantDialectQualificationRead struct {
	Kind    string             `json:"kind"`
	Key     string             `json:"key"`
	Digest  string             `json:"digest"`
	Mapping inputRecordDialect `json:"mapping"`
}

func (e *assistantRunExecution) readAssistantDialectRecipe(ctx context.Context, name string, a assistantDialectRecipeArguments) (runcontext.ToolResult, error) {
	if err := e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	var value any
	switch name {
	case "get_dialect_recipe_instructions":
		value = struct {
			Owner    string `json:"owner"`
			Flow     string `json:"flow"`
			Guidance string `json:"guidance"`
		}{"Desk", assistantDialectRecipeFlow, assistantDialectRecipeGuidance}
	case "list_dialects":
		items, err := e.service.server.preparationRepository().List(ctx)
		if err != nil {
			return runcontext.ToolResult{}, err
		}
		value = struct {
			Items []inputPreparationSummary `json:"items"`
		}{items}
	case "read_dialect_recipe":
		saved, err := e.service.server.preparationRepository().Get(ctx, a.ID)
		if err != nil {
			return runcontext.ToolResult{}, err
		}
		d := saved.Document
		view := assistantDialectRecipeRead{ID: d.ID, Name: d.Name, Version: d.RecipeVersion, Revision: saved.SHA256, Intent: d.Recipe.Intent, Template: d.Recipe.Template}
		if r := d.Recipe.Research; r != nil {
			view.Research = &assistantDialectResearchRead{r.Fields, r.Criteria, r.Limits, r.Execution}
		}
		if q := d.Recipe.Qualification; q != nil {
			view.Qualification = &assistantDialectQualificationRead{q.Kind, q.Key, q.Digest, q.Mapping}
		}
		value = view
	default:
		return runcontext.ToolResult{}, errAssistantRunInvalid
	}
	if err := e.Authorize(ctx); err != nil {
		return runcontext.ToolResult{}, err
	}
	return assistantHostJSON(value)
}

const assistantDialectRecipeFlow = "Instructions and supplied sources → research → Records in Dialects → reviewed mapping → pack or graph → decision results."

const assistantDialectRecipeGuidance = "Every dialect researches records. Describe the domain in intent and requested fields; there is no target mode to choose. Ask for unclear requirements rather than inventing them. Put total record counts and request budgets only in the structured limits object, not in intent or criteria. Intent describes the target and research task; structured limits are authoritative if existing prose disagrees. Requested fields describe what to research, never invented values. Prefer text, integer, decimal, boolean, text-list and range. Integers are JSON integer numbers; decimals are exact decimal strings without exponents; lists are arrays of strings; ranges have min/max endpoints, both integer numbers or both decimal strings. Preserve saved string and number fields when updating an existing recipe unless the user requests a type change. Criteria guide research; qualificationTarget is only a suggestion and does not implement a rule. A pack is optional. For a dialect that translates record columns into pack facts, read_dialect and propose_dialect provide a reviewable proposal with explicit conversions; do not put bindings in a recipe patch. People at selected companies needs a human-reviewed company selection. Reuse enabled Desk connections, selected by the person during review. Use propose_dialect_recipe to design a new dialect. For an existing input, read_dialect_recipe and propose_dialect_action retain a reviewed update, removal or run action. Read records and activity with their Desk tools when needed. Do not emit a pack proposal fence or claim the input was saved, researched, verified or evaluated. Saving and starting are separate user actions. Search is through existing Gateway connections; supplied URLs are also supported. No native web capability or contact verification is implied."
