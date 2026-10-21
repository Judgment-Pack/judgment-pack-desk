package desk

import (
	"context"
	"encoding/json"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantDialectRecordSchemas() map[string]json.RawMessage {
	object := func(p map[string]any, required ...string) any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": p, "required": required, "additionalProperties": false}
	}
	id := map[string]any{"type": "string", "pattern": inputPreparationID.String()}
	integer := func(lo, hi int) any { return map[string]any{"type": "integer", "minimum": lo, "maximum": hi} }
	// Share the published portable recipe property schemas, not their create-only
	// required list. A patch replaces complete field/criterion arrays when present.
	var recipe map[string]json.RawMessage
	_ = json.Unmarshal(assistantDialectRecipeSchemas()["propose_dialect_recipe"], &recipe)
	var props map[string]any
	_ = json.Unmarshal(recipe["properties"], &props)
	research := map[string]any{}
	for _, k := range []string{"fields", "criteria", "limits", "execution"} {
		research[k] = props[k]
	}
	research["target"] = map[string]any{"type": "string", "enum": []string{"records"}}
	patch := object(map[string]any{"name": props["name"], "intent": props["intent"], "research": object(research)})
	action := object(map[string]any{"id": id, "revision": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "kind": map[string]any{"type": "string", "enum": []string{"update-recipe", "remove-recipe", "research", "reprocess", "stop-research", "evaluate"}}, "patch": patch, "section": map[string]any{"type": "string", "enum": []string{"research", "qualification", "all"}}, "recordIds": map[string]any{"type": "array", "minItems": 1, "maxItems": 1000, "uniqueItems": true, "items": map[string]any{"type": "string", "pattern": assistantDialectRecordID.String()}}, "runId": map[string]any{"type": "string", "pattern": inputResearchID.String()}}, "id", "revision", "kind")
	return map[string]json.RawMessage{"read_dialect_records": assistantChatJSON(object(map[string]any{"id": id, "offset": integer(0, 1000), "limit": integer(1, 10)}, "id")), "read_dialect_activity": assistantChatJSON(object(map[string]any{"id": id}, "id")), "propose_dialect_action": assistantChatJSON(action)}
}
func (e *assistantRunExecution) assistantDialectRecordTools() (*assistantHostRegistry, error) {
	schemas := assistantDialectRecordSchemas()
	tools := []runcontext.HostTool{}
	descriptions := map[string]string{"read_dialect_records": "Read bounded current Dialect records, original supplied strings and separate typed research, source/run references and current versus stale human review. No values are independently verified and no review is saved.", "read_dialect_activity": "Read bounded local preparation, research and evaluation history. No worker is recovered, started, stopped, retried or authorized.", "propose_dialect_action": "Retain one review-only recipe update/removal or research/reprocess/stop/evaluate card against an explicit current root recipe read. Portable content only. Never saves a recipe, changes records, starts/stops work or grants approval."}
	for _, name := range assistantDialectRecordNames {
		name := name
		tool := runcontext.HostTool{Name: name, Description: descriptions[name], InputSchema: schemas[name], Effect: "read", Delegation: "read", ContextPolicy: &runcontext.ToolContextPolicy{Reference: true}}
		if name == "propose_dialect_action" {
			tool.Effect = "proposal"
			tool.Delegation = ""
			tool.ContextPolicy = nil
		}
		tool.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			a, err := readAssistantDialectRecordArguments(name, raw)
			if err != nil {
				return assistantDialectFailure(err), nil
			}
			return e.invokeObservedHost(ctx, name, raw, func(ctx context.Context, root bool) (assistantHostObservation, error) {
				switch name {
				case "read_dialect_records":
					result, err := e.readAssistantDialectRecords(ctx, a)
					return assistantHostObservation{Result: result}, err
				case "read_dialect_activity":
					result, err := e.readAssistantDialectActivity(ctx, a)
					return assistantHostObservation{Result: result}, err
				default:
					if !root || e.record.Binding.Phase != "work" {
						return assistantHostObservation{}, errAssistantRunScope
					}
					return e.executeAssistantDialectAction(ctx, a)
				}
			})
		}
		tools = append(tools, tool)
	}
	return newAssistantHostRegistry(tools)
}
