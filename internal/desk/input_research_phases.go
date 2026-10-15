package desk

import (
	"context"
	"fmt"
	"strings"
)

type researchActionError struct{ error }

// Completion belongs to the host. A bounded reprocessing selection is done
// when every selected row has been published (or confirmed unchanged), even
// when its retained evidence leaves fields unknown. Those gaps affect the
// completion status, not the need for another model request. Discovery keeps
// its existing goal/enrichment behavior.
func researchWorkComplete(run inputResearchRun) bool {
	if researchNeedsCorrection(run) || run.PublicationPending {
		return false
	}
	if !run.Reprocess {
		return len(run.Records) >= run.Recipe.Limits.Records && !researchHasUnresolvedFields(run)
	}
	if len(run.Selection) == 0 {
		return false
	}
	done := map[string]bool{}
	for _, outcome := range run.Outcomes {
		if outcome.State == "updated" || outcome.State == "unchanged" {
			done[outcome.RecordID] = true
		}
	}
	for _, selected := range run.Selection {
		if !done[selected.RecordID] {
			return false
		}
	}
	return true
}

// Finalization uses already retained sources, so it has no search dependency.
// AI identity is still checked before every new model request.
func validateResearchSearchForPhase(ctx context.Context, run inputResearchRun, ops inputResearchOperations) error {
	if run.Phase == "finalize" {
		return nil
	}
	return ops.validateSearch(ctx, run.Recipe)
}

// Keep the host's stopping condition visible before the model's explanation.
// A request ceiling is independent of the record ceiling.
func researchFinishProblem(run inputResearchRun, explanation string) error {
	if len(run.Records) >= run.Recipe.Limits.Records && !researchHasUnresolvedFields(run) {
		return nil
	}
	explanation = strings.TrimSpace(explanation)
	hostReason := ""
	if len(run.Records) < run.Recipe.Limits.Records {
		if run.Reprocess {
			hostReason = "Only selected records were processed using saved sources. No searches or website reads were made."
		} else if run.ContinuationOf != "" {
			hostReason = "This continuation used saved sources only. Start a new research run to discover more sources."
		} else if run.Phase == "finalize" {
			hostReason = run.PhaseReason + " Review the limits in Recipe before starting a new run."
		}
	}
	if hostReason == "" && explanation == "" {
		return nil
	}
	parts := []string{fmt.Sprintf("Saved %d of up to %d records.", len(run.Records), run.Recipe.Limits.Records)}
	if hostReason != "" {
		parts = append(parts, hostReason)
	}
	if explanation != "" {
		parts = append(parts, "AI explanation: "+explanation)
	}
	return fmt.Errorf("%s", strings.Join(parts, " "))
}
