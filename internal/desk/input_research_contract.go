package desk

// Research owns discovery, never qualification. Every model-produced value
// remains generated, including an extraction with a supporting quotation.
import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/delegation"
	"net/url"
	"regexp"
	"strings"
)

type inputResearchLimits struct {
	Records    int `json:"records"`
	Searches   int `json:"searches"`
	Pages      int `json:"pages"`
	ModelCalls int `json:"modelCalls"`
}

// Missing policy preserves the no-retry behavior of existing recipes.
type inputResearchRetryPolicy struct {
	Mode           string `json:"mode"`
	MaxRetries     int    `json:"maxRetries"`
	SearchTimeouts bool   `json:"searchTimeouts"`
}
type inputResearchAI struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Model      string `json:"model"`
	Effort     string `json:"effort,omitempty"`
}
type inputResearchSearch struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
}
type inputResearchField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}
type inputResearchCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	// Search intent cannot silently become a qualification rule.
	QualificationTarget string `json:"qualificationTarget,omitempty"`
}
type inputResearchCompanyRef struct {
	PreparationID string `json:"preparationId"`
	Revision      string `json:"revision"`
	EntityID      string `json:"entityId"`
}
type inputResearchRecipe struct {
	Execution     *delegation.Policy        `json:"execution,omitempty"`
	CompanyInputs []inputResearchCompanyRef `json:"companyInputs,omitempty"`
	Version       int                       `json:"version"`
	Target        string                    `json:"target"`
	Fields        []inputResearchField      `json:"fields"`
	Criteria      []inputResearchCriterion  `json:"criteria"`
	CompanyIDs    []string                  `json:"companyIds,omitempty"`
	AI            inputResearchAI           `json:"ai"`
	Search        *inputResearchSearch      `json:"search,omitempty"`
	Limits        inputResearchLimits       `json:"limits"`
	Retries       *inputResearchRetryPolicy `json:"retries,omitempty"`
}
type inputResearchCitation struct {
	SourceID string `json:"sourceId"`
	Quote    string `json:"quote"`
}
type inputResearchValue struct {
	Value   json.RawMessage         `json:"value,omitempty"`
	Status  string                  `json:"status"`
	Sources []inputResearchCitation `json:"sources"`
	Note    string                  `json:"note,omitempty"`
}
type inputResearchEntity struct {
	Website   string                        `json:"website,omitempty"`
	ID        string                        `json:"id"`
	Kind      string                        `json:"kind"`
	Name      string                        `json:"name"`
	URL       string                        `json:"url"`
	CompanyID string                        `json:"companyId,omitempty"`
	Fields    map[string]inputResearchValue `json:"fields"`
}
type inputResearchSource struct {
	TextTruncated bool     `json:"textTruncated,omitempty"`
	Bytes         int      `json:"bytes"`
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	URLs          []string `json:"urls,omitempty"`
	URL           string   `json:"url,omitempty"`
	Query         string   `json:"query,omitempty"`
	At            string   `json:"at"`
	Digest        string   `json:"digest"`
	Text          string   `json:"text"`
	// This is source acquisition verification, never factual truth.
	Verification string `json:"verification"`
}

var researchFieldName = regexp.MustCompile(`^[a-z][a-zA-Z0-9_]{0,63}$`)

func validateInputResearch(r *inputResearchRecipe) error {
	if r == nil {
		return nil
	}
	bad := errors.New("Research requires a supported target, named AI connection, requested fields and bounded limits.")
	if r.Version != 1 || (r.Target != "records" && r.Target != "companies" && r.Target != "people" && r.Target != "people-at-companies") {
		return bad
	}
	if !aiIDPattern.MatchString(r.AI.Connection) || r.AI.Revision == "" || len(r.AI.Revision) > 256 || strings.TrimSpace(r.AI.Model) == "" || len(r.AI.Model) > 128 || strings.ContainsAny(r.AI.Model, "\r\n\x00") {
		return bad
	}
	if !contains([]string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"}, r.AI.Effort) {
		return bad
	}
	if r.Search != nil && (!researchSourceName.MatchString(r.Search.Connection) || r.Search.Revision == "" || len(r.Search.Revision) > 256) {
		return bad
	}
	if p := r.Retries; p != nil {
		if (p.Mode != "off" && p.Mode != "temporary") || p.MaxRetries < 0 || p.MaxRetries > 3 || (p.Mode == "temporary" && p.MaxRetries < 1) || (p.Mode == "off" && p.SearchTimeouts) {
			return errors.New("Choose Off or Temporary failures, with one to three retries. Timed-out searches require an enabled retry policy.")
		}
	}
	if r.Execution != nil {
		if err := r.Execution.Validate(); err != nil {
			return err
		}
	}
	l := r.Limits
	if l.Records < 1 || l.Records > 50 || l.Searches < 0 || l.Searches > 30 || l.Pages < 0 || l.Pages > 100 || l.ModelCalls < 1 || l.ModelCalls > 60 {
		return bad
	}
	if l.Searches > 0 && r.Search == nil {
		return errors.New("Choose an existing search connection, or set the search limit to zero to research supplied URLs.")
	}
	if len(r.Fields) < 1 || len(r.Fields) > 30 || len(r.Criteria) > 30 || len(r.CompanyIDs)+len(r.CompanyInputs) > 50 {
		return bad
	}
	if r.Target == "people-at-companies" && len(r.CompanyIDs)+len(r.CompanyInputs) == 0 || r.Target != "people-at-companies" && len(r.CompanyIDs)+len(r.CompanyInputs) > 0 {
		return errors.New("People at selected companies requires a reviewed company selection.")
	}
	seen := map[string]bool{}
	for _, id := range r.CompanyIDs {
		if id == "" || len(id) > 128 || seen[id] {
			return bad
		}
		seen[id] = true
	}
	for _, ref := range r.CompanyInputs {
		if !inputPreparationID.MatchString(ref.PreparationID) || ref.Revision == "" || len(ref.Revision) > 256 || ref.EntityID == "" || len(ref.EntityID) > 128 || seen[ref.EntityID] {
			return bad
		}
		seen[ref.EntityID] = true
	}
	seen = map[string]bool{}
	for _, f := range r.Fields {
		if !researchFieldName.MatchString(f.Name) || contains([]string{"constructor", "prototype", "__proto__"}, f.Name) || seen[f.Name] || strings.TrimSpace(f.Description) == "" || len(f.Description) > 1000 || !contains([]string{"string", "number", "boolean"}, f.Type) {
			return bad
		}
		seen[f.Name] = true
	}
	seen = map[string]bool{}
	for _, c := range r.Criteria {
		if !researchFieldName.MatchString(c.ID) || seen[c.ID] || strings.TrimSpace(c.Description) == "" || len(c.Description) > 2000 || len(c.QualificationTarget) > 512 {
			return bad
		}
		seen[c.ID] = true
	}
	return nil
}

// A URL is an identity candidate, not permission to fetch. Gateway still owns
// public-address, redirect and credential admission for every actual read.
func inputResearchURL(raw string) (string, error) {
	if len(raw) > 4096 || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\x00\\") {
		return "", errors.New("Invalid research URL.")
	}
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Opaque != "" {
		return "", errors.New("Research URLs must be complete HTTP or HTTPS URLs without credentials.")
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// validateResearchEntity checks host-retained references, not model assertions
// about sources. Unsupported values remain explicit; a quotation never upgrades
// a model value into a verified fact or a qualification outcome.
func validateResearchEntity(entity *inputResearchEntity, recipe inputResearchRecipe, sources map[string]inputResearchSource, allowedURLs map[string]bool, companyIDs map[string]bool) error {
	if entity.Kind != "record" && entity.Kind != "company" && entity.Kind != "person" {
		return errors.New("Unknown research entity type.")
	}
	if entity.Kind != researchTargetKind(recipe.Target) {
		return errors.New("Research result does not match the selected target.")
	}
	if strings.TrimSpace(entity.Name) == "" || len(entity.Name) > 240 {
		return errors.New("Research result requires a name.")
	}
	normalized, e := inputResearchURL(entity.URL)
	if e != nil || !allowedURLs[normalized] {
		return errors.New("A record URL must come from supplied inputs or retained search/page results.")
	}
	entity.URL = normalized
	if recipe.Target == "people-at-companies" && !companyIDs[entity.CompanyID] || entity.CompanyID != "" && !companyIDs[entity.CompanyID] {
		return errors.New("The company relationship must name a reviewed company record.")
	}
	identity := normalized
	if entity.Kind == "company" {
		u, _ := url.Parse(normalized)
		u.Host = strings.TrimPrefix(u.Host, "www.")
		identity = u.String()
	}
	entity.ID = "entity_" + digestOf([]byte(entity.Kind+"\n"+identity))
	if len(entity.Fields) != len(recipe.Fields) {
		return errors.New("Every requested field must have a value or an explicit unresolved state.")
	}
	for _, field := range recipe.Fields {
		v, ok := entity.Fields[field.Name]
		if !ok {
			return fmt.Errorf("Missing requested field: %s", field.Name)
		}
		if !contains([]string{"supported", "interpretation", "missing", "conflicting", "inaccessible", "stale"}, v.Status) || len(v.Note) > 2000 || len(v.Sources) > 8 {
			return errors.New("Invalid field status or source references.")
		}
		if v.Status == "missing" || v.Status == "inaccessible" || v.Status == "conflicting" {
			if len(v.Value) > 0 && string(v.Value) != "null" {
				return errors.New("Unresolved fields cannot silently supply a value.")
			}
		} else {
			var value any
			if len(v.Value) == 0 || len(v.Value) > 8192 || json.Unmarshal(v.Value, &value) != nil || value == nil {
				return errors.New("Resolved fields require a typed value.")
			}
			switch field.Type {
			case "string":
				if _, ok := value.(string); !ok {
					return errors.New("Expected text.")
				}
			case "number":
				if _, ok := value.(float64); !ok {
					return errors.New("Expected a number.")
				}
			case "boolean":
				if _, ok := value.(bool); !ok {
					return errors.New("Expected yes or no.")
				}
			}
		}
		if v.Status == "supported" && len(v.Sources) == 0 {
			return errors.New("A supported value requires a retained page quotation.")
		}
		// Keep the model's original quotation in the action journal. In saved
		// records, use the exact source span after matching layout whitespace.
		citations := append([]inputResearchCitation(nil), v.Sources...)
		for i, cite := range citations {
			source, exists := sources[cite.SourceID]
			quote, matches := researchSourceQuote(source.Text, cite.Quote)
			if !exists || !matches {
				return errors.New("A source quotation must occur in the retained source text.")
			}
			if v.Status == "supported" && (source.Kind != "page" || source.Verification != "verified") {
				return errors.New("Search snippets and unchecked responses cannot support a page-backed value.")
			}
			citations[i].Quote = quote
		}
		v.Sources = citations
		entity.Fields[field.Name] = v
	}
	return nil
}

// A retained research snapshot is explicitly generated. Its identity is separate
// from the record snapshot used by a historical evaluation.
type inputResearchRecord struct {
	RunID         string              `json:"runId"`
	RecipeVersion int                 `json:"recipeVersion"`
	Entity        inputResearchEntity `json:"entity"`
}

// Generic records retain their individual evidence URLs. Company identity is
// the only target allowed to deduplicate across paths on the same website.
func researchTargetKind(target string) string {
	switch target {
	case "companies":
		return "company"
	case "people", "people-at-companies":
		return "person"
	default:
		return "record"
	}
}
