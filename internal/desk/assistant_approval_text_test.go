package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Authored requests and expected parsed forms are shared with the rendered card.
// Nothing in this fixture is recorded from an execution of the implementation.
type assistantApprovalTextCase struct {
	Label   string          `json:"label"`
	Request string          `json:"request"`
	Args    json.RawMessage `json:"args"`
	Digest  string          `json:"digest"`
}

func assistantApprovalTextCases(t *testing.T) []assistantApprovalTextCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "approval-text.json"))
	assistantRunMust(t, err)
	var rows []assistantApprovalTextCase
	assistantRunMust(t, json.Unmarshal(raw, &rows))
	if len(rows) != 4 {
		t.Fatal("missing approval text acceptance cases")
	}
	return rows
}

// This reuses the actual API adapter and actual native Manager/callback paths,
// original canonical store, Gate and custom-source effect callback, with exact raw requests.
// The default Runtime fixture retains its separate supported v3 approval coverage.
// It does not certify live providers or a released dependency until executed.
func TestAssistantApprovalTextActualAPIAndNative(t *testing.T) {
	for _, engine := range []string{"api", "native"} {
		for _, row := range assistantApprovalTextCases(t) {
			if !t.Run(engine+"/"+row.Label, func(t *testing.T) { assistantRuntimeOwnerContinuation(t, engine, "positive", row) }) {
				return
			}
		}
	}
}

func TestAssistantApprovalTextRecordReaders(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "approval-records.json"))
	assistantRunMust(t, err)
	var rows []struct {
		Checkpoint json.RawMessage `json:"checkpoint"`
		Event      json.RawMessage `json:"event"`
	}
	assistantRunMust(t, json.Unmarshal(raw, &rows))
	for _, row := range rows {
		value, err := runcontext.ParseJSON(row.Checkpoint)
		assistantRunMust(t, err)
		cp, err := runcontext.ReadExecution(value)
		assistantRunMust(t, err)
		var event contract.RunEvent
		assistantRunMust(t, json.Unmarshal(row.Event, &event))
		_, err = assistantRunEventBytes(event)
		assistantRunMust(t, err)
		if cp == nil || cp.Approval == nil || event.ApprovalRequested == nil || runcontext.ApprovalShownArguments(*cp.Approval) != `{"id":9007199254740993}` || runcontext.ApprovalEventArguments(*event.ApprovalRequested) != `{"id":9007199254740993}` {
			t.Fatal("record reader lost kept text")
		}
		var changed map[string]any
		assistantRunMust(t, json.Unmarshal(row.Checkpoint, &changed))
		changed["version"] = 7
		bad, err := json.Marshal(changed)
		assistantRunMust(t, err)
		value, err = runcontext.ParseJSON(bad)
		assistantRunMust(t, err)
		if _, err = runcontext.ReadExecution(value); err == nil {
			t.Fatal("unknown checkpoint version admitted")
		}
	}
}
