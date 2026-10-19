package desk

import (
	"encoding/json"
	"testing"
)

func TestResearchAttemptsDoNotReplayCompletedOrUnknownCharges(t *testing.T) {
	run := inputResearchRun{Recipe: researchRecipeFixture(), State: "running"}
	run.Recipe.Limits.Searches = 2
	args := json.RawMessage(`{"query":"suppliers"}`)
	a, cached, e := beginResearchAttempt(&run, "search", args)
	if e != nil || cached {
		t.Fatal(e)
	}
	if _, _, e = beginResearchAttempt(&run, "search", args); e == nil {
		t.Fatal("inflight acquisition replayed")
	}
	if e = finishResearchAttempt(&run, a.ID, json.RawMessage(`{"hits":[]}`), ""); e != nil {
		t.Fatal(e)
	}
	if b, cached, e := beginResearchAttempt(&run, "search", args); e != nil || !cached || string(b.Result) != `{"hits":[]}` {
		t.Fatal("completed response not reused", e)
	}
	a, _, e = beginResearchAttempt(&run, "search", json.RawMessage(`{"query":"different"}`))
	if e != nil {
		t.Fatal(e)
	}
	if e = finishResearchAttempt(&run, a.ID, nil, "Response was lost."); e != nil {
		t.Fatal(e)
	}
	if _, _, e = beginResearchAttempt(&run, "search", json.RawMessage(`{"query":"different"}`)); e == nil {
		t.Fatal("uncertain charge repeated")
	}
	if _, _, e = beginResearchAttempt(&run, "search", json.RawMessage(`{"query":"third"}`)); e == nil {
		t.Fatal("failed attempt did not consume budget")
	}
}
func TestResearchRestartKeepsRecordsAndRequiresExplicitContinuation(t *testing.T) {
	run := inputResearchRun{Recipe: researchRecipeFixture(), State: "running", Records: []inputResearchEntity{{ID: "entity-1"}}}
	if _, _, e := beginResearchAttempt(&run, "model", json.RawMessage(`{}`)); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(run)
	if e != nil {
		t.Fatal(e)
	}
	var restored inputResearchRun
	if e = json.Unmarshal(raw, &restored); e != nil {
		t.Fatal(e)
	}
	recoverInputResearch(&restored)
	if restored.State != "interrupted" || len(restored.Records) != 1 || restored.Attempts[0].State != "uncertain" {
		t.Fatalf("unsafe restart: %+v", restored)
	}
	restored.StopRequested = true
	if _, _, e = beginResearchAttempt(&restored, "model", json.RawMessage(`{"new":true}`)); e == nil {
		t.Fatal("stopped research admitted new work")
	}
}
