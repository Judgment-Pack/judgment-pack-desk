package desk

import (
	"encoding/json"
	"fmt"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Project the original admitted checkpoint, before the display-only checkpoint
// drops capability fields. Running/paused state cannot become a terminal report.
func assistantCanonicalRunReport(checkpoint *contract.Checkpoint) (json.RawMessage, error) {
	if checkpoint == nil || (checkpoint.Status != "complete" && checkpoint.Status != "stopped") {
		return nil, nil
	}
	report, err := runcontext.ProjectRunReport(*checkpoint)
	if err != nil {
		return nil, err
	}
	text, err := runcontext.CanonicalRunReport(report)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(text), nil
}

// RawMessage retains numeric spelling and duplicate report member names until
// the published strict reader checks them. No decode/re-encode validates input.
// Legacy summary-only histories do not acquire any new required fields.
func validateConversationRunReports(raw []byte) error {
	var chat struct {
		Checkpoint struct {
			State struct {
				Responses []struct {
					Work struct {
						Report struct {
							Canonical json.RawMessage `json:"canonical"`
						} `json:"report"`
					} `json:"work"`
				} `json:"responses"`
			} `json:"state"`
		} `json:"checkpoint"`
	}
	if err := json.Unmarshal(raw, &chat); err != nil {
		return fmt.Errorf("invalid conversation run report: %w", err)
	}
	for _, response := range chat.Checkpoint.State.Responses {
		rawReport := response.Work.Report.Canonical
		if rawReport == nil {
			continue
		}
		if _, err := runcontext.ParseRunReport(rawReport); err != nil {
			return fmt.Errorf("invalid conversation run report: %w", err)
		}
	}
	return nil
}
