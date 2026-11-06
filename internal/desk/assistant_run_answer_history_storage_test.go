package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAssistantAnswerSuccessorHistoryStorageShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "assistant", "serverRuns", "fixtures", "answer-successor.json"))
	assistantRunMust(t, err)
	var vector struct {
		History []json.RawMessage `json:"answerHistory"`
	}
	assistantRunMust(t, json.Unmarshal(raw, &vector))
	// Every case starts with the shared handwritten vector, including its exact
	// answer, and changes only the named wire shape.
	for _, tc := range []struct {
		name   string
		valid  bool
		mutate func(map[string]json.RawMessage, map[string]json.RawMessage, map[string]json.RawMessage)
	}{
		{name: "paired-null", valid: true},
		{name: "paired-strings", valid: true, mutate: func(_, _ map[string]json.RawMessage, q map[string]json.RawMessage) {
			q["recommendedOptionId"], q["recommendation"] = json.RawMessage(`"two"`), json.RawMessage(`"Use the second scope."`)
		}},
		{name: "unknown-origin-authority", mutate: func(o, _, _ map[string]json.RawMessage) { o["approval"] = json.RawMessage(`true`) }},
		{name: "unknown-question-authority", mutate: func(_, e, _ map[string]json.RawMessage) { e["checkpoint"] = json.RawMessage(`{}`) }},
		{name: "unknown-question-member", mutate: func(_, _ map[string]json.RawMessage, q map[string]json.RawMessage) {
			q["approval"] = json.RawMessage(`true`)
		}},
		{name: "unknown-option-member", mutate: func(_, _ map[string]json.RawMessage, q map[string]json.RawMessage) {
			var options []map[string]json.RawMessage
			assistantRunMust(t, json.Unmarshal(q["options"], &options))
			options[0]["approval"] = json.RawMessage(`true`)
			q["options"] = assistantChatJSON(options)
		}},
		{name: "null-question-version", mutate: func(_, e, _ map[string]json.RawMessage) { e["version"] = json.RawMessage(`null`) }},
		{name: "unknown-answer-member", mutate: func(o, _, _ map[string]json.RawMessage) {
			a, err := assistantTestObject(o["answer"])
			assistantRunMust(t, err)
			a["approved"] = json.RawMessage(`true`)
			o["answer"] = assistantChatJSON(a)
		}},
		{name: "missing-answer-text", mutate: func(o, _, _ map[string]json.RawMessage) {
			a, err := assistantTestObject(o["answer"])
			assistantRunMust(t, err)
			delete(a, "text")
			o["answer"] = assistantChatJSON(a)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := assistantTestObject(vector.History[0])
			assistantRunMust(t, err)
			e, err := assistantTestObject(o["question"])
			assistantRunMust(t, err)
			q, err := assistantTestObject(e["question"])
			assistantRunMust(t, err)
			if tc.mutate != nil {
				tc.mutate(o, e, q)
			}
			e["question"] = assistantChatJSON(q)
			o["question"] = assistantChatJSON(e)
			if err := readAssistantRunAnswerHistory(assistantChatJSON([]any{o})); (err == nil) != tc.valid {
				t.Fatalf("history valid=%v: %v", tc.valid, err)
			}
		})
	}
	for _, key := range []string{"recommendedOptionId", "recommendation"} {
		for _, value := range []string{"missing", `false`, `1`, `[]`, `{}`, `""`, `"two"`} {
			t.Run(key+"/"+value, func(t *testing.T) {
				o, err := assistantTestObject(vector.History[0])
				assistantRunMust(t, err)
				e, err := assistantTestObject(o["question"])
				assistantRunMust(t, err)
				q, err := assistantTestObject(e["question"])
				assistantRunMust(t, err)
				if value == "missing" {
					delete(q, key)
				} else {
					q[key] = json.RawMessage(value)
				}
				e["question"] = assistantChatJSON(q)
				o["question"] = assistantChatJSON(e)
				if readAssistantRunAnswerHistory(assistantChatJSON([]any{o})) == nil {
					t.Fatal("missing, malformed or unpaired recommendation accepted")
				}
			})
		}
	}
	for _, invalid := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`{}`), assistantChatJSON(make([]json.RawMessage, assistantRunCountLimit+1))} {
		if readAssistantRunAnswerHistory(invalid) == nil {
			t.Fatal("invalid history container or retention bound accepted")
		}
	}
}
