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
