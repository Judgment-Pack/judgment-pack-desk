package desk

// Portable research shares the durable research journal and operation accounting,
// but freezes the original Runner input and resolved request. It is never encoded
// as a local recipe, and model output cannot replace its contract or permission.
import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type portableResearchPlan struct {
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Profile   string          `json:"profile"`
	Source    string          `json:"source"`
	Arguments json.RawMessage `json:"arguments"`
}
type portableResearchProfile struct {
	Profile json.RawMessage `json:"profile"`
	Digest  string          `json:"digest"`
}
type portableResearchFrozen struct {
	Input          json.RawMessage                    `json:"input"`
	Plan           portableResearchPlan               `json:"plan"`
	Choice         backgroundModelChoice              `json:"choice"`
	Limits         inputResearchLimits                `json:"limits"`
	Profiles       []portableResearchProfile          `json:"profiles"`
	SearchBindings map[string]searchBindingDescriptor `json:"searchBindings,omitempty"`
	ChatID         string                             `json:"chatId,omitempty"`
	SessionScope   string                             `json:"sessionScope"`
}
type portableResearchJournal struct {
	Frozen portableResearchFrozen `json:"frozen"`
	// Response is R's acquisition envelope, accepted by Runner against Frozen.
	// It is a generated caller assertion, not proof that its claims are true.
	Response json.RawMessage `json:"response,omitempty"`
}
type portableRetainedResponse struct {
	Profile struct {
		ID     string `json:"id"`
		Digest string `json:"digest"`
	} `json:"profile"`
	Request json.RawMessage `json:"request"`
	Result  json.RawMessage `json:"result"`
	Receipt json.RawMessage `json:"receipt"`
	Salts   json.RawMessage `json:"salts"`
}

func portableResearchRevision(frozen portableResearchFrozen) string {
	raw, _ := json.Marshal(frozen)
	return sha256Digest(raw)
}
func validatePortableResearchJournal(run inputResearchRun, id string) error {
	bad := errors.New("Invalid portable research journal.")
	if run.Version != 2 || run.Portable == nil || run.ID != id || !inputResearchID.MatchString(id) || run.PreparationID != "" || len(run.Attempts) > 64 || len(run.Records) != 0 || len(run.Sources) != 0 || len(run.Tasks) != 0 || run.Approval != nil || len(run.PendingRetries) != 0 || run.PendingRetry != nil || run.Execution != nil || run.Reprocess || run.ContinuationOf != "" || len(run.Candidates) != 0 || len(run.Selection) != 0 {
		return bad
	}
	frozen := run.Portable.Frozen
	if run.Revision != portableResearchRevision(frozen) || !json.Valid(frozen.Input) || len(frozen.Input) > 1<<20 || !json.Valid(frozen.Plan.Arguments) || frozen.Plan.Kind != "research" || frozen.Plan.Name == "" || frozen.Plan.Profile == "" || frozen.Plan.Source != modelSourceName || len(frozen.Profiles) < 1 || len(frozen.Profiles) > 3 || frozen.SessionScope == "" || (frozen.ChatID != "" && !researchChatID.MatchString(frozen.ChatID)) {
		return bad
	}
	if !aiIDPattern.MatchString(frozen.Choice.Connection) || frozen.Choice.Revision == "" || frozen.Choice.Model == "" || len(frozen.Choice.Model) > 256 || !contains([]string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max"}, frozen.Choice.Effort) {
		return bad
	}
	limits := frozen.Limits
	if validatePortableResearchLimits(limits) != nil {
		return bad
	}
	// Legacy recipe members cannot authorize portable execution accidentally.
	legacy, _ := json.Marshal(run.Recipe)
	empty, _ := json.Marshal(inputResearchRecipe{})
	if !bytes.Equal(legacy, empty) {
		return bad
	}
	if !contains([]string{"review", "queued", "running", "completed", "interrupted", "stopped", "blocked", "partial", "paused"}, run.State) {
		return bad
	}
	for _, timestamp := range []string{run.CreatedAt, run.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return bad
		}
	}
	seen := map[string]bool{}
	for _, entry := range frozen.Profiles {
		var profile struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(entry.Profile, &profile) != nil || profile.ID == "" || seen[profile.ID] || !strings.HasPrefix(entry.Digest, "sha256:") || len(entry.Digest) != 71 {
			return bad
		}
		seen[profile.ID] = true
	}
	if len(frozen.SearchBindings) > 1 {
		return bad
	}
	for id, binding := range frozen.SearchBindings {
		if !seen[id] || binding.Version != 1 || binding.Shape != "http" || binding.Adapter.Name != "adapter-sources" || len(binding.FixedArguments) != 2 || !researchSourceName.MatchString(binding.FixedArguments["connection"]) || !researchConnectionRevision.MatchString(binding.FixedArguments["revision"]) || !json.Valid(binding.RequestSchema) {
			return bad
		}
	}
	if !seen[frozen.Plan.Profile] {
		return bad
	}
	if err := validateResearchBudgets(run); err != nil {
		return err
	}
	limits = researchApprovedLimits(run)
	used := researchBudgetUsage(run)
	if used.ModelCalls > limits.ModelCalls || used.Searches > limits.Searches || used.Pages > limits.Pages {
		return bad
	}
	if len(run.Portable.Response) > 0 && (!json.Valid(run.Portable.Response) || len(run.Portable.Response) > portableResearchResponseLimit) {
		return bad
	}
	if run.State == "completed" && len(run.Portable.Response) == 0 {
		return bad
	}
	return nil
}

// Retained acquisitions remain in completed operation order, including model
// requests that proposed another action. No final response can erase them.
func portableResearchResponses(run inputResearchRun) ([]portableRetainedResponse, error) {
	responses := []portableRetainedResponse{}
	for _, attempt := range run.Attempts {
		if attempt.State != "completed" {
			continue
		}
		var response portableRetainedResponse
		if decodeDataJSON(attempt.Result, &response) != nil || response.Profile.ID == "" || response.Profile.Digest == "" || !json.Valid(response.Request) || !json.Valid(response.Result) || !json.Valid(response.Receipt) || !json.Valid(response.Salts) {
			return nil, errors.New("A retained portable research acquisition is incomplete.")
		}
		responses = append(responses, response)
	}
	return responses, nil
}

// Preserve bounded retained JSON without HTML-escaping every raw result again.
func portableResearchJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func validatePortableResearchLimits(limits inputResearchLimits) error {
	if limits.Records != 1 || limits.ModelCalls < 1 || limits.ModelCalls > 30 || limits.Searches < 0 || limits.Searches > 15 || limits.Pages < 0 || limits.Pages > 15 || limits.ModelCalls+limits.Searches+limits.Pages > 64 {
		return errors.New("Portable research requires one record, up to 30 model requests, 15 searches and 15 page reads.")
	}
	return nil
}
