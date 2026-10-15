package desk

import (
	"encoding/json"
	"errors"
	"os"
)

func readInputEvaluation(root *os.Root) (inputEvaluation, error) {
	var b inputEvaluation
	raw, e := readPrivateData(root, "record.json", inputEvaluationLimit)
	if e == nil {
		e = decodeDataJSON(raw, &b)
	}
	if e == nil && (b.Version != 1 || !inputEvaluationID.MatchString(b.ID) || !inputPreparationID.MatchString(b.PreparationID) || len(b.Rows) > inputRecordLimit) {
		e = errors.New("Invalid evaluation record.")
	}
	return b, e
}
func writeInputEvaluation(root *os.Root, b inputEvaluation) error {
	raw, e := json.Marshal(b)
	if e != nil {
		return e
	}
	if len(raw) > inputEvaluationLimit {
		return errors.New("This evaluation exceeds its storage limit.")
	}
	return writePrivateData(root, "record.json", raw)
}
