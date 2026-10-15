package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Exercise discovery, evidence validation, incremental publication and limits
// together. Larger record goals must not inherit a three-record prompt ceiling.
func TestResearchRecordGoalAndRequestBudgetsAreIndependent(t *testing.T) {
	for _, budget := range []int{15, 40} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			root, run := researchRunFixture(t)
			run.Intent = "Find up to three companies in Canada."
			run.Recipe.Limits.Records = 10
			run.Recipe.Limits.Searches = 10
			run.Recipe.Limits.Pages = 20
			run.Recipe.Limits.ModelCalls = budget
			run.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: strings.Repeat("a", 64)}
			if err := writeInputResearch(root, run); err != nil {
				t.Fatal(err)
			}
			ops := recoveryOperations()
			models, searches, pages, published := 0, 0, 0, 0
			sourceID := ""
			ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
				var state struct {
					Phase   string                `json:"phase"`
					Records []inputResearchEntity `json:"savedRecords"`
					Goal    map[string]int        `json:"recordGoal"`
				}
				if err := json.Unmarshal([]byte(prompt), &state); err != nil {
					t.Fatal(err)
				}
				if state.Goal["maximum"] != 10 {
					t.Fatal("free-text count replaced the saved limit")
				}
				n := models
				models++
				if state.Phase == "finalize" {
					return recoveryAction("finish", map[string]string{"reason": "No more pages can be read in finalization."}), nil
				}
				url := fmt.Sprintf("https://company%d.example/", len(state.Records))
				switch n % 3 {
				case 0:
					return recoveryAction("search", map[string]string{"query": fmt.Sprintf("Company %d Canada", len(state.Records))}), nil
				case 1:
					return recoveryAction("read", map[string]string{"url": url}), nil
				default:
					record := hardeningRecord(url, sourceID)
					record["name"] = fmt.Sprintf("Company %d", len(state.Records))
					return recoveryAction("records", map[string]any{"records": []any{record}}), nil
				}
			}
			ops.source = func(_ context.Context, current inputResearchRun, a inputResearchAttempt, _ json.RawMessage) (inputResearchSource, error) {
				url := fmt.Sprintf("https://company%d.example/", len(current.Records))
				if a.Kind == "search" {
					searches++
				} else {
					pages++
					sourceID = a.ID
				}
				return inputResearchSource{ID: a.ID, Kind: a.Kind, URL: url, URLs: []string{url}, Text: "Canada", Verification: "verified"}, nil
			}
			ops.publish = func(current inputResearchRun) error { published = len(current.Records); return nil }
			executeInputResearch(context.Background(), fileResearchStore(root), run, ops)
			saved, err := readInputResearch(root)
			if err != nil {
				t.Fatal(err)
			}
			if models > budget || searches > 10 || pages > 20 || published != len(saved.Records) || saved.Recipe.Limits != run.Recipe.Limits {
				t.Fatal("budget or publication changed", models, searches, pages, published)
			}
			if budget == 40 {
				if len(saved.Records) != 10 || saved.State != "completed" || models != 30 {
					t.Fatalf("ten supported companies were not saved: records=%d state=%s models=%d problem=%s", len(saved.Records), saved.State, models, saved.Problem)
				}
			} else if len(saved.Records) != 4 || saved.State != "paused" || !strings.Contains(saved.Problem, "Source discovery stopped after 13 of 15 AI requests") {
				t.Fatalf("budget shortfall not explained: records=%d state=%s models=%d problem=%s", len(saved.Records), saved.State, models, saved.Problem)
			}
		})
	}
}
