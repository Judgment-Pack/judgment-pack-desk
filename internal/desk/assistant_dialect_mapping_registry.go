package desk

import (
	"context"
	"encoding/json"
	"sort"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantDialectSchemas() map[string]json.RawMessage {
	text := func(max int) any { return map[string]any{"type": "string", "maxLength": max, "minLength": 1} }
	object := func(fields map[string]any) json.RawMessage {
		required := []string{}
		for key := range fields {
			required = append(required, key)
		}
		sort.Strings(required)
		return assistantChatJSON(map[string]any{"type": "object", "properties": fields, "required": required, "additionalProperties": false})
	}
	id := map[string]any{"type": "string", "pattern": "^prep_[a-f0-9]{32}$"}
	revision := map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}
	conversion := map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"text", "decimal", "number", "boolean", "research-value"}}, "trueValue": text(160), "falseValue": text(160)}, "required": []string{"kind"}, "additionalProperties": false}
	facts := map[string]any{"type": "array", "maxItems": 1000, "items": json.RawMessage(object(map[string]any{"target": text(1024), "column": text(1024), "conversion": conversion}))}
	evidence := map[string]any{"type": "array", "maxItems": 1000, "items": json.RawMessage(object(map[string]any{"requirement": text(1024), "column": text(1024)}))}
	variant := map[string]any{"type": "string", "enum": []string{"asserted", "generated"}}
	return map[string]json.RawMessage{
		"read_dialect":                object(map[string]any{"id": id, "packKey": text(512)}),
		"propose_dialect":             object(map[string]any{"id": id, "revision": revision, "packKey": text(512), "packDigest": map[string]any{"type": "string", "pattern": "^sha256:[a-f0-9]{64}$"}, "facts": facts, "evidence": evidence, "summary": text(4000)}),
		"read_dialect_publication":    object(map[string]any{"id": id, "variant": variant}),
		"propose_dialect_publication": object(map[string]any{"id": id, "variant": variant, "revision": revision, "version": text(256), "impact": json.RawMessage(object(map[string]any{"classification": map[string]any{"type": "string", "enum": []string{"initial", "patch", "minor", "major"}}, "rationale": text(4000)}))}),
	}
}
func (e *assistantRunExecution) assistantDialectTools() (*assistantHostRegistry, error) {
	schemas := assistantDialectSchemas()
	tools := []runcontext.HostTool{}
	descriptions := map[string]string{
		"read_dialect":                "Read a saved Dialect's safe columns, bounded samples, typed field declarations and exact saved pack fact/evidence requirements. Required before proposing a mapping. No sources or decisions run.",
		"propose_dialect":             "Check and retain complete replacement mapping bindings and explicit conversions against an exact current root read. Preview only; preserve original typed values and omissions. No recipe, records, decisions, research or jobs change.",
		"read_dialect_publication":    "Read the current asserted or generated mapping revision and immutable publication base. Source classes remain separate. Runtime owns version choices; existing jobs keep their pins.",
		"propose_dialect_publication": "Check one exact mapping version and explicit compatibility rationale against the current root read. Retains candidate/review evidence only; never publishes, grants an allowance, edits a recipe or changes a job pin.",
	}
	for _, name := range assistantDialectNames {
		name := name
		tool := runcontext.HostTool{Name: name, Description: descriptions[name], InputSchema: schemas[name], Effect: "read", Delegation: "read", ContextPolicy: &runcontext.ToolContextPolicy{Reference: true}}
		if name == "propose_dialect" || name == "propose_dialect_publication" {
			tool.Effect = "proposal"
			tool.Delegation = ""
			tool.ContextPolicy = nil
		}
		tool.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
			a, err := readAssistantDialectArguments(name, raw)
			if err != nil {
				return assistantDialectFailure(err), nil
			}
			return e.invokeObservedHost(ctx, name, raw, func(ctx context.Context, root bool) (assistantHostObservation, error) {
				if name == "read_dialect" || name == "read_dialect_publication" {
					read, err := e.readAssistantDialect(ctx, a, name == "read_dialect_publication")
					if err != nil {
						return assistantHostObservation{}, err
					}
					result, err := assistantHostJSON(json.RawMessage(read.View))
					return assistantHostObservation{Result: result, DialectRead: &read}, err
				}
				if !root || e.record.Binding.Phase != "work" {
					return assistantHostObservation{}, errAssistantRunScope
				}
				return e.executeAssistantDialectProposal(ctx, name, a)
			})
		}
		tools = append(tools, tool)
	}
	return newAssistantHostRegistry(tools)
}
