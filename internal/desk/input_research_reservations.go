package desk

import "errors"

var errResearchStorageLimit = errors.New("The saved-source storage limit leaves no room for another request. Save records from retained evidence.")

func researchAttemptByID(run *inputResearchRun, id string) (*inputResearchAttempt, error) {
	for i := range run.Attempts {
		if run.Attempts[i].ID == id {
			return &run.Attempts[i], nil
		}
	}
	return nil, errors.New("Research attempt not found.")
}
func researchReservedBytes(run inputResearchRun) int {
	total := 0
	for _, a := range run.Attempts {
		total += a.ReservedBytes
	}
	return total
}

// Reserve the maximum retained proof plus bounded metadata before acquisition.
// Admission and release happen under the journal lock; concurrent pages cannot
// each spend the same free bytes. Fewer reads fit as storage fills.
func reserveResearchSource(run *inputResearchRun, attempt *inputResearchAttempt) error {
	if run.Portable != nil {
		return reservePortableResearchJournal(run, attempt)
	}
	if attempt.Kind == "model" {
		return nil
	}
	allowance := inputResearchSourceProofLimit + (2 << 20)
	if run.RetrievedBytes < 0 || run.RetrievedBytes+researchReservedBytes(*run)+allowance > inputResearchRetainedByteLimit {
		return errResearchStorageLimit
	}
	attempt.ReservedBytes = allowance
	return nil
}

// Portable acquisitions live in the journal, including refused results. Measure
// its actual encoding (raw JSON is compacted without HTML escaping), then reserve
// a maximum retained result and terminal metadata for every admitted attempt.
// A separate reserve covers the final answer and run state. Admission happens
// under the same journal lock as the started attempt; no request spends it twice.
const portableResearchAttemptReserve = (2 << 20) + (64 << 10)
const portableResearchFinalReserve = portableResearchResponseLimit + (64 << 10)

var errPortableResearchJournalSize = errors.New("The research journal storage limit leaves no room for another acquisition. Completed and refused acquisitions were kept; no further request was made.")

func reservePortableResearchJournal(run *inputResearchRun, attempt *inputResearchAttempt) error {
	candidate := *run
	candidate.Attempts = append(append([]inputResearchAttempt(nil), run.Attempts...), *attempt)
	raw, err := portableResearchJSON(candidate)
	if err != nil {
		return err
	}
	if len(raw)+researchReservedBytes(*run)+portableResearchAttemptReserve+portableResearchFinalReserve > inputResearchRecordLimit {
		return errPortableResearchJournalSize
	}
	attempt.ReservedBytes = portableResearchAttemptReserve
	return nil
}

// Diagnostic prose is host metadata, not signed evidence. Bound its encoded
// size so control-character escaping cannot spend the terminal reservation.
func portableResearchProblemText(problem string) string {
	raw, err := portableResearchJSON(problem)
	if err != nil || len(raw) > 8<<10 {
		return "The research operation failed. Its diagnostic details exceeded the journal metadata limit. Retained acquisitions were kept."
	}
	return problem
}
