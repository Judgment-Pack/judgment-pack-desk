package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"

	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

var assistantDialectRecordNames = []string{"read_dialect_records", "read_dialect_activity", "propose_dialect_action"}

func assistantDialectRecordTool(name string) bool { return contains(assistantDialectRecordNames, name) }

var assistantDialectRecordID = regexp.MustCompile(`^row_[a-f0-9]{64}$`)

type assistantDialectRecipePatch struct {
	Name     *string                        `json:"name,omitempty"`
	Intent   *string                        `json:"intent,omitempty"`
	Research *assistantDialectResearchPatch `json:"research,omitempty"`
}
type assistantDialectResearchPatch struct {
	Target    *string                          `json:"target,omitempty"`
	Fields    *[]inputResearchField            `json:"fields,omitempty"`
	Criteria  *[]inputResearchCriterion        `json:"criteria,omitempty"`
	Limits    *inputResearchLimits             `json:"limits,omitempty"`
	Execution *assistantDialectRecipeExecution `json:"execution,omitempty"`
}
type assistantDialectRecordArguments struct {
	ID        string                       `json:"id"`
	Revision  string                       `json:"revision,omitempty"`
	Offset    *int                         `json:"offset,omitempty"`
	Limit     *int                         `json:"limit,omitempty"`
	Kind      string                       `json:"kind,omitempty"`
	Patch     *assistantDialectRecipePatch `json:"patch,omitempty"`
	Section   string                       `json:"section,omitempty"`
	RecordIDs []string                     `json:"recordIds,omitempty"`
	RunID     string                       `json:"runId,omitempty"`
}

func readAssistantDialectRecordArguments(name string, raw []byte) (assistantDialectRecordArguments, error) {
	var a assistantDialectRecordArguments
	bad := func() (assistantDialectRecordArguments, error) {
		return a, failure.ToolInput("Use the declared Dialect identity, exact recipe revision and kind-specific portable fields. No recipe, records or jobs were changed.")
	}
	if len(raw) > 1<<20 || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil {
		return bad()
	}
	allowed := []string{"id"}
	switch name {
	case "read_dialect_records":
		allowed = append(allowed, "offset", "limit")
	case "read_dialect_activity":
	case "propose_dialect_action":
		allowed = append(allowed, "revision", "kind", "patch", "section", "recordIds", "runId")
	default:
		return bad()
	}
	fields, err := assistantTestControls(raw, allowed...)
	if err != nil || readAssistantDialectCustody(raw, reflect.TypeOf(a)) != nil || json.Unmarshal(raw, &a) != nil || !inputPreparationID.MatchString(a.ID) {
		return bad()
	}
	for _, v := range fields {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return bad()
		}
	}
	if name != "propose_dialect_action" {
		if a.Offset != nil && (*a.Offset < 0 || *a.Offset > 1000) || a.Limit != nil && (*a.Limit < 1 || *a.Limit > 10) {
			return bad()
		}
		return a, nil
	}
	if !assistantRunDigest(a.Revision) {
		return bad()
	}
	kindAllowed := []string{"id", "revision", "kind"}
	switch a.Kind {
	case "update-recipe":
		kindAllowed = append(kindAllowed, "patch")
		if a.Patch == nil || !validAssistantDialectPatch(*a.Patch) {
			return bad()
		}

		patch, _ := assistantTestObject(fields["patch"])
		if researchRaw := patch["research"]; researchRaw != nil {
			research, _ := assistantTestObject(researchRaw)
			if rowsRaw := research["criteria"]; rowsRaw != nil {
				rows, err := assistantChatArray(rowsRaw)
				if err != nil {
					return bad()
				}
				for _, row := range rows {
					members, err := assistantTestObject(row)
					if err != nil {
						return bad()
					}
					if value := members["qualificationTarget"]; value != nil {
						target, err := assistantTestString(value)
						if err != nil || !assistantDialectRecipeText(target, 512) {
							return bad()
						}
					}
				}
			}
		}
	case "remove-recipe":
		kindAllowed = append(kindAllowed, "section")
		if !contains([]string{"research", "qualification", "all"}, a.Section) {
			return bad()
		}
	case "research":
	case "reprocess", "evaluate":
		kindAllowed = append(kindAllowed, "recordIds")
		if len(a.RecordIDs) < 1 || len(a.RecordIDs) > 1000 {
			return bad()
		}
		seen := map[string]bool{}
		for _, id := range a.RecordIDs {
			if !assistantDialectRecordID.MatchString(id) || seen[id] {
				return bad()
			}
			seen[id] = true
		}
	case "stop-research":
		kindAllowed = append(kindAllowed, "runId")
		if !inputResearchID.MatchString(a.RunID) {
			return bad()
		}
	default:
		return bad()
	}
	for key := range fields {
		if !contains(kindAllowed, key) {
			return bad()
		}
	}
	return a, nil
}
func validAssistantDialectPatch(p assistantDialectRecipePatch) bool {
	if p.Name == nil && p.Intent == nil && p.Research == nil {
		return false
	}
	if p.Name != nil && !assistantDialectRecipeText(*p.Name, 160) || p.Intent != nil && !assistantDialectRecipeText(*p.Intent, 12000) {
		return false
	}
	r := p.Research
	if r == nil {
		return true
	}
	if r.Target == nil && r.Fields == nil && r.Criteria == nil && r.Limits == nil && r.Execution == nil {
		return false
	}
	a := assistantDialectRecipeContent{Name: "Review", Intent: "Review", Target: "records", Fields: []inputResearchField{{Name: "description", Description: "Description", Type: "string"}}, Criteria: []inputResearchCriterion{}, Limits: inputResearchLimits{Records: 3, Searches: 0, Pages: 3, ModelCalls: 10}}
	if r.Target != nil {
		a.Target = *r.Target
	}
	if r.Fields != nil {
		a.Fields = *r.Fields
	}
	if r.Criteria != nil {
		a.Criteria = *r.Criteria
	}
	if r.Limits != nil {
		a.Limits = *r.Limits
	}
	a.Execution = r.Execution
	return validAssistantDialectRecipeContent(a)
}

func assistantDialectProposal(name string) bool {
	return contains([]string{"propose_dialect", "propose_dialect_publication", "propose_dialect_action"}, name)
}

// Shared finite action custody includes the workflow without calling it a proposal.
func assistantDialectActionProducer(name string) bool {
	return assistantDialectProposal(name) || name == "request_dialect_research"
}
