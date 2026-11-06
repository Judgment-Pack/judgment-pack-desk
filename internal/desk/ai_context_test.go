package desk

import (
	"encoding/json"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/aicontext"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResearchContextRetrievesExactRetainedPagesWithoutSourceAcquisition(t *testing.T) {
	text := strings.Repeat("Canadian company 😀 ", 5000)
	run := inputResearchRun{Sources: []inputResearchSource{{ID: "allowed", Kind: "page", Text: text}, {ID: "sibling", Kind: "page", Text: "other task"}}}
	scoped := run
	scoped.Sources = run.Sources[:1]
	offset := 0
	assembled := ""
	for {
		args, _ := json.Marshal(map[string]any{"sourceId": "allowed", "sha256": digestOf([]byte(text)), "offset": offset})
		got, err := researchContextRead(scoped, args)
		if err != nil {
			t.Fatal(err)
		}
		part := got["text"].(string)
		if !utf8.ValidString(part) || len(part) > aicontext.Defaults().ReadBytes {
			t.Fatal("invalid page")
		}
		assembled += part
		if got["nextOffset"] == nil {
			break
		}
		offset = got["nextOffset"].(int)
	}
	if assembled != text {
		t.Fatal("source changed during paging")
	}
	for _, args := range []string{`{"sourceId":"sibling","sha256":"wrong","offset":0}`, `{"sourceId":"allowed","sha256":"stale","offset":0}`} {
		if _, err := researchContextRead(scoped, []byte(args)); err == nil {
			t.Fatal("out-of-scope/stale source accepted")
		}
	}
	raw, _ := json.Marshal(map[string]any{"tool": "read_context", "arguments": map[string]any{"sourceId": "allowed", "sha256": digestOf([]byte(text)), "offset": 6000}})
	run.Attempts = []inputResearchAttempt{{TaskID: "task", Kind: "model", State: "completed", Result: raw}}
	if researchContextSelection(run, "") != nil {
		t.Fatal("parent inherited child read")
	}
	// A successful local read is represented in the next prompt, with no search/page operation.
	raw, _ = json.Marshal(map[string]any{"tool": "read_context", "arguments": map[string]any{"sourceId": "allowed", "sha256": digestOf([]byte(text)), "offset": 0}})
	run.Attempts[0].Result = raw
	if researchContextSelection(run, "task") == nil {
		t.Fatal("requested context omitted")
	}
}
func TestResearchContextCompactionPreservesAuthorityAndOriginals(t *testing.T) {
	body := map[string]any{"approvedLimits": map[string]any{"records": 10}, "retainedSources": []any{map[string]any{"id": "source", "text": strings.Repeat("quotation 😀 ", 20000)}}, "retainedSourceIndex": []any{map[string]any{"sourceId": "source", "sha256": "exact"}}, "recordResults": []any{map[string]any{"status": "saved", "id": "record-1"}}}
	raw, _ := json.Marshal(body)
	task := backgroundFixture(backgroundModelChoice{})
	task.Prompt = string(raw)
	_, projection, err := backgroundResearchOpening(task)
	if err != nil {
		t.Fatal(err)
	}
	replacements, err := projection.Project(t.Context(), runcontext.OpeningProjectionInput{Fit: func(previews map[string]string) (runcontext.OpeningFit, error) {
		return runcontext.OpeningFit{Fits: len(previews) == 1 && len(previews["/input/retainedSources/0/text"]) < 4000}, nil
	}})
	if err != nil || len(replacements) != 1 || !strings.HasPrefix(body["retainedSources"].([]any)[0].(map[string]any)["text"].(string), replacements["/input/retainedSources/0/text"]) {
		t.Fatal("invalid source-only projection", err)
	}
	unchanged, _ := json.Marshal(body)
	if string(raw) != string(unchanged) {
		t.Fatal("changed original journal")
	}
}
