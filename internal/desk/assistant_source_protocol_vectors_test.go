package desk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestAssistantSourceDecisionSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/assistant-source-decisions.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Version int `json:"version"`
		Cases   []struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			Raw        string `json:"raw"`
			PadToBytes int    `json:"padToBytes"`
			Accept     bool   `json:"accept"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.Version != 1 || len(vectors.Cases) == 0 {
		t.Fatal("missing shared source decision vectors")
	}
	seen := map[string]bool{}
	for _, row := range vectors.Cases {
		if row.ID == "" || seen[row.ID] {
			t.Fatal("duplicate or empty vector ID")
		}
		seen[row.ID] = true
		t.Run(row.ID, func(t *testing.T) {
			body := row.Raw
			if row.PadToBytes > 0 {
				if row.PadToBytes < len(body) {
					t.Fatal("invalid byte padding")
				}
				body += strings.Repeat(" ", row.PadToBytes-len(body))
			}
			var target any
			switch row.Kind {
			case "approve":
				target = &assistantSourceApproveRequest{}
			case "revoke":
				target = &assistantSourceRevokeRequest{}
			default:
				t.Fatal("unknown vector request kind")
			}
			err := readAssistantSourceRequest(strings.NewReader(body), target)
			if (err == nil) != row.Accept {
				t.Fatalf("accept=%v, error=%v", row.Accept, err)
			}
		})
	}
}
