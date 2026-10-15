package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestResearchShortReferencesPreserveExactOpaqueURLs(t *testing.T) {
	_, run := researchRunFixture(t)
	exact := "https://vertexaisearch.cloud.google.com/grounding-api-redirect/opaque-token=="
	run.Seeds = []string{exact}
	run.Sources = []inputResearchSource{{ID: "search", URLs: []string{exact, "https://example.com/page"}}}
	refs := researchURLReferences(run)
	if len(refs) != 2 || refs[0].URL != exact {
		t.Fatal("references lost exact URLs")
	}
	action := inputResearchAction{Tool: "read", Arguments: json.RawMessage(`{"urlRef":"url_1"}`)}
	kind, args, err := researchSourceArguments(run, action)
	if err != nil || kind != "page" {
		t.Fatal(err)
	}
	var got struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(args, &got)
	if got.URL != exact {
		t.Fatal("redirect token altered")
	}
	for _, bad := range []string{`{"urlRef":"url_404"}`, `{"url":"` + strings.TrimSuffix(exact, "=") + `"}`, `{"urlRef":"url_1","url":"https://attacker.example/"}`} {
		if _, _, err := researchSourceArguments(run, inputResearchAction{Tool: "read", Arguments: json.RawMessage(bad)}); !errors.Is(err, errResearchURLReference) {
			t.Fatal("unknown/mismatched URL accepted", err)
		}
	}
	run.Sources = append(run.Sources, inputResearchSource{URLs: []string{"https://aaa.example/", exact}})
	if next := researchURLReferences(run); next[0] != refs[0] || next[1] != refs[1] {
		t.Fatal("refs changed after source append")
	}
	prompt, err := researchPrompt(run)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		URLs []inputResearchURLReference `json:"availableURLs"`
	}
	_ = json.Unmarshal([]byte(prompt), &input)
	if len(input.URLs) != 3 || input.URLs[0].URL != exact {
		t.Fatal("prompt lacks exact refs")
	}
	if len(run.Sources[0].URLs) != 2 {
		t.Fatal("prompt modified retained source")
	}
}

func TestResearchCorrectsAlteredReferenceAndSavesRecords(t *testing.T) {
	root, run := researchRunFixture(t)
	exact := "https://vertexaisearch.cloud.google.com/grounding-api-redirect/opaque-token=="
	run.Seeds = []string{exact}
	run.Recipe.Limits.Records = 10
	run.Recipe.Limits.ModelCalls = 6
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	ops := recoveryOperations()
	models, pages, published := 0, 0, 0
	sourceID := ""
	ops.model = func(_ context.Context, _ inputResearchAI, prompt string, _ func(bool) error) (json.RawMessage, error) {
		models++
		switch models {
		case 1:
			return recoveryAction("read", map[string]string{"url": strings.TrimSuffix(exact, "=")}), nil
		case 2:
			var input struct {
				Feedback  string                      `json:"actionFeedback"`
				URLs      []inputResearchURLReference `json:"availableURLs"`
				Remaining map[string]int              `json:"requestsRemaining"`
			}
			if err := json.Unmarshal([]byte(prompt), &input); err != nil {
				t.Fatal(err)
			}
			if input.Feedback == "" || len(input.URLs) != 1 || input.Remaining["model"] != 5 || input.Remaining["page"] != run.Recipe.Limits.Pages {
				t.Fatal("missing recovery context or incorrect usage")
			}
			return recoveryAction("read", map[string]string{"urlRef": input.URLs[0].Ref}), nil
		case 3:
			return recoveryAction("records", map[string]any{"records": []any{map[string]any{"kind": "company", "name": "Example", "url": exact, "fields": map[string]any{"country": map[string]any{"value": "Canada", "status": "supported", "sources": []any{map[string]string{"sourceId": sourceID, "quote": "Canada"}}}}}}}), nil
		default:
			return recoveryAction("finish", map[string]any{}), nil
		}
	}
	ops.source = func(_ context.Context, _ *os.Root, _ inputResearchRun, a inputResearchAttempt, args json.RawMessage) (inputResearchSource, error) {
		pages++
		var request struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(args, &request)
		if request.URL != exact {
			t.Fatal("altered URL reached provider")
		}
		sourceID = a.ID
		return inputResearchSource{ID: a.ID, Kind: "page", URL: exact, URLs: []string{exact}, Text: "Canada", Verification: "verified"}, nil
	}
	ops.publish = func(r inputResearchRun) error {
		if len(r.Records) > 0 {
			published++
		}
		return nil
	}
	executeInputResearch(context.Background(), root, run, ops)
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "completed-with-gaps" || len(saved.Records) != 1 || pages != 1 || models != 4 || published == 0 {
		t.Fatalf("state %s records %d pages %d models %d published %d", saved.State, len(saved.Records), pages, models, published)
	}
	if saved.Attempts[0].ActionProblem == "" || researchActivity(saved).Steps[0].ActionProblem == "" || saved.Attempts[0].State != "completed" {
		t.Fatal("rejection missing from activity")
	}
	if len(researchSourceFailures(saved)) != 0 || saved.PendingRetry != nil {
		t.Fatal("local rejection misreported as a source failure/retry")
	}
}

func TestResearchInvalidReferenceCorrectionsStayBounded(t *testing.T) {
	for _, limit := range []int{2, 10} {
		root, run := researchRunFixture(t)
		run.Recipe.Limits.ModelCalls = limit
		if err := writeInputResearch(root, run); err != nil {
			t.Fatal(err)
		}
		ops := recoveryOperations()
		models := 0
		ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
			models++
			return recoveryAction("read", map[string]string{"urlRef": "unknown"}), nil
		}
		ops.source = func(context.Context, *os.Root, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
			t.Fatal("rejected source was fetched")
			return inputResearchSource{}, nil
		}
		executeInputResearch(context.Background(), root, run, ops)
		saved, err := readInputResearch(root)
		if err != nil {
			t.Fatal(err)
		}
		if models != min(limit, 3) || len(saved.Attempts) != models {
			t.Fatal("correction exceeded limits", models)
		}
		if limit == 2 && saved.State != "partial" || limit == 10 && saved.State != "partial" {
			t.Fatal("wrong terminal state", saved.State)
		}
	}
}
