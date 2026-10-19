package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
)

func readInputResearch(root *os.Root) (inputResearchRun, error) {
	var run inputResearchRun
	raw, e := readPrivateData(root, "record.json", inputResearchRecordLimit)
	if e == nil {
		e = decodeDataJSON(raw, &run)
	}
	if e == nil && (run.Version != 1 || !inputResearchID.MatchString(run.ID) || !inputPreparationID.MatchString(run.PreparationID) || len(run.Records) > 50 || len(run.Attempts) > 250 || len(run.Sources) > 130) {
		e = errors.New("Invalid research record.")
	}
	if e == nil {
		e = validateInputResearch(&run.Recipe)
	}
	if e == nil {
		e = validateResearchBudgets(run)
	}
	if e == nil {
		e = validateResearchApproval(run)
	}
	if e == nil {
		e = validateResearchCandidates(run)
	}
	if e == nil {
		e = validateResearchRecordScope(run)
	}
	if e == nil {
		e = validateResearchTasks(run)
	}
	return run, e
}
func writeInputResearch(root *os.Root, run inputResearchRun) error {
	if e := validateResearchTasks(run); e != nil {
		return e
	}
	if e := validateResearchRecordScope(run); e != nil {
		return e
	}
	raw, e := json.Marshal(run)
	if e != nil {
		return e
	}
	if len(raw) > inputResearchRecordLimit {
		return errors.New("Research storage limit reached.")
	}
	return writePrivateData(root, "record.json", raw)
}

// Fixture helpers preserve historical file tests; feature code cannot call them.
func changeInputResearch(root *os.Root, change func(*inputResearchRun) error) (inputResearchRun, error) {
	run, err := readInputResearch(root)
	if err != nil {
		return inputResearchRun{}, err
	}
	return (fileResearchJournal{root: root, id: run.ID}).Update(context.Background(), change)
}

func fileResearchStore(root *os.Root) researchRunStore {
	// A broken fixture journal produces an invalid binding and refuses work.
	run, _ := readInputResearch(root)
	return (&Server{}).researchRunStore(root, run.ID)
}
