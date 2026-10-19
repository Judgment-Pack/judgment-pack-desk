package desk

// Recipe bindings retain an exact, user-selected pack snapshot. They are not
// a reviewed-set lock, a test result, or permission to run a decision. This
// supplied-input slice accepts only mapping-v2 case copies; generated sources
// must use verified source profiles before they can enter this boundary.
import (
	"encoding/json"
	"errors"
	"strings"
)

type inputDialectFact struct {
	Target string `json:"target"`
	Source string `json:"source"`
}
type inputDialectEvidence struct {
	Requirement string `json:"requirement"`
	Source      string `json:"source"`
}
type inputCaseDialect struct {
	Facts    []inputDialectFact     `json:"facts"`
	Evidence []inputDialectEvidence `json:"evidence"`
}
type inputRecordDialect struct {
	Version int               `json:"version"`
	Case    *inputCaseDialect `json:"case,omitempty"`
	// Raw entries are refused below. Keeping the array makes this directly
	// consumable by the existing shared mapping editor and Runner v2 contract.
	Sources          []json.RawMessage `json:"sources"`
	Unmapped         []string          `json:"unmapped,omitempty"`
	UnmappedEvidence []string          `json:"unmappedEvidence,omitempty"`
}
type inputQualification struct {
	Conversions map[string]inputConversion `json:"conversions,omitempty"`
	Kind        string                     `json:"kind"`
	Key         string                     `json:"key"`
	Pack        string                     `json:"pack"`
	Digest      string                     `json:"digest"`
	Mapping     inputRecordDialect         `json:"mapping"`
}

func inputPointerValid(p string) bool {
	if !strings.HasPrefix(p, "/") || len(p) > 1024 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' {
			if i+1 >= len(p) || p[i+1] != '0' && p[i+1] != '1' {
				return false
			}
			i++
		}
	}
	for _, part := range strings.Split(p[1:], "/") {
		if part == "__proto__" || part == "constructor" || part == "prototype" {
			return false
		}
	}
	return true
}
func validateInputQualification(q *inputQualification) error {
	if q == nil {
		return nil
	}
	fail := func() error { return errors.New("Choose a saved pack and review its dialect.") }
	if q.Kind != "pack" || strings.TrimSpace(q.Key) == "" || len(q.Key) > 512 || len(q.Pack) > 1<<20 || q.Digest != "sha256:"+digestOf([]byte(q.Pack)) {
		return fail()
	}
	var pack map[string]any
	if rejectDuplicateMembers([]byte(q.Pack)) != nil || json.Unmarshal([]byte(q.Pack), &pack) != nil || pack == nil {
		return fail()
	}
	// Identity belongs to the retained bytes, not a title supplied separately.
	if id, ok := pack["id"].(string); !ok || id == "" {
		return fail()
	}
	if _, ok := pack["decision"].(map[string]any); !ok {
		return fail()
	}
	if version, ok := pack["version"].(string); !ok || version == "" {
		return fail()
	}
	for _, field := range []string{"title", "description"} {
		if value, exists := pack[field]; exists {
			if _, ok := value.(string); !ok {
				return fail()
			}
		}
	}
	facts, evidence := map[string]bool{}, map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["op"] == "fact" {
				if p, ok := x["path"].(string); ok {
					facts[p] = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(pack)
	if value, exists := pack["evidenceRequirements"]; exists {
		rows, ok := value.([]any)
		if !ok {
			return fail()
		}
		for _, value := range rows {
			row, ok := value.(map[string]any)
			if !ok {
				return fail()
			}
			id, ok := row["id"].(string)
			if !ok || id == "" || evidence[id] {
				return fail()
			}
			if description, exists := row["description"]; exists {
				if _, ok := description.(string); !ok {
					return fail()
				}
			}
			evidence[id] = true
		}
	}
	m := q.Mapping
	if m.Version != 2 || len(m.Sources) > 0 || len(m.Unmapped) > 1000 || len(m.UnmappedEvidence) > 1000 {
		return fail()
	}
	targets, requirements := map[string]bool{}, map[string]bool{}
	if m.Case != nil {
		if len(m.Case.Facts) > 1000 || len(m.Case.Evidence) > 1000 {
			return fail()
		}
		for _, f := range m.Case.Facts {
			if !facts[f.Target] || !inputPointerValid(f.Target) || !inputPointerValid(f.Source) || !strings.HasPrefix(f.Source, "/values/") || strings.Contains(strings.TrimPrefix(f.Source, "/values/"), "/") || targets[f.Target] {
				return fail()
			}
			for prior := range targets {
				if strings.HasPrefix(prior, f.Target+"/") || strings.HasPrefix(f.Target, prior+"/") {
					return fail()
				}
			}
			targets[f.Target] = true
		}
		for _, e := range m.Case.Evidence {
			if !evidence[e.Requirement] || requirements[e.Requirement] || !inputPointerValid(e.Source) || !strings.HasPrefix(e.Source, "/values/") || strings.Contains(strings.TrimPrefix(e.Source, "/values/"), "/") {
				return fail()
			}
			requirements[e.Requirement] = true
		}
	}
	for _, target := range m.Unmapped {
		if !facts[target] || targets[target] {
			return fail()
		}
		targets[target] = true
	}
	for _, requirement := range m.UnmappedEvidence {
		if !evidence[requirement] || requirements[requirement] {
			return fail()
		}
		requirements[requirement] = true
	}
	if q.Mapping.Sources == nil {
		q.Mapping.Sources = []json.RawMessage{}
	}
	if c := q.Mapping.Case; c != nil {
		if c.Facts == nil {
			c.Facts = []inputDialectFact{}
		}
		if c.Evidence == nil {
			c.Evidence = []inputDialectEvidence{}
		}
	}
	return validateInputConversions(q)
}
