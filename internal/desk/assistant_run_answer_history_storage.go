package desk

import (
	"encoding/json"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
)

// History has a closed Desk envelope, but its question uses Agent's nullable
// wire contract. The generic custody reader cannot reflect that generated type.
func readAssistantRunAnswerHistory(raw []byte) error {
	if len(raw) > assistantRunRecordLimit {
		return errAssistantRunInvalid
	}
	rows, err := assistantChatArray(raw)
	if err != nil || len(rows) > assistantRunCountLimit {
		return errAssistantRunInvalid
	}
	for _, row := range rows {
		var origin struct {
			ParentRunID string             `json:"parentRunId"`
			AdmissionID string             `json:"admissionId"`
			ActionID    string             `json:"actionId"`
			Question    json.RawMessage    `json:"question"`
			Answer      assistantRunAnswer `json:"answer"`
		}
		if readAssistantDialectCustody(row, reflect.TypeOf(origin)) != nil || json.Unmarshal(row, &origin) != nil {
			return errAssistantRunInvalid
		}
		var question struct {
			Type     string          `json:"type"`
			ID       string          `json:"id"`
			Question json.RawMessage `json:"question"`
			Version  *int64          `json:"version,omitempty"`
		}
		if readAssistantDialectCustody(origin.Question, reflect.TypeOf(question)) != nil || json.Unmarshal(origin.Question, &question) != nil || question.Type != "user_question" || !assistantChatID.MatchString(question.ID) {
			return errAssistantRunInvalid
		}
		// The saved-history reader requires both recommendation members, accepts
		// their paired nulls or valid strings, and checks closed options and bounds.
		if _, err := turn.ReadUserQuestion(question.Question); err != nil {
			return errAssistantRunInvalid
		}
	}
	return nil
}
