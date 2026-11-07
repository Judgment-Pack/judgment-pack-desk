package desk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// These values are handwritten independently of the canonical writer. The
// request and valueJSON strings retain the original lexical evidence while
// arbitrary parsed objects may acquire canonical member order in storage.
func assistantCheckpointEqualityFixture() *contract.Checkpoint {
	request := `{"rehearsal":true,"value":{"id":9007199254740993,"amount":10.50}}`
	source := `{"turns":[{"z":1,"a":2}],"questions":[]}`
	digest := "sha256:" + strings.Repeat("a", 64)
	return &contract.Checkpoint{
		Version: 5, ID: "checkpoint-equality", RequestKey: strings.Repeat("b", 64),
		Phase: "work", Status: "paused", Unit: "model-requests", Limit: 20,
		Digest: &digest,
		Context: contract.CheckpointContext{Scope: "equality", Entries: []contract.CheckpointRetainedEntry{{
			ID: "equality:conversation:fixture", Kind: "history", Label: "Conversation",
			Value: json.RawMessage(source), ValueJSON: &source, Bytes: int64(len(source)), Digest: strings.Repeat("c", 64),
		}}},
		Approval: &contract.CheckpointApproval{
			CallID: "call", Name: "write_note", Tool: "notes__write_note", State: "asked",
			Args:    map[string]json.RawMessage{"rehearsal": json.RawMessage(`true`), "value": json.RawMessage(`{"id":9007199254740992,"amount":10.5}`)},
			Request: &request,
		},
	}
}

func TestAssistantCheckpointEqualityCanonicalObjectOrder(t *testing.T) {
	left, right := assistantCheckpointEqualityFixture(), assistantCheckpointEqualityFixture()
	right.Context.Entries[0].Value = json.RawMessage(`{"questions":[],"turns":[{"a":2,"z":1}]}`)
	right.Approval.Args["value"] = json.RawMessage(`{"amount":10.5,"id":9007199254740992}`)
	beforeLeft, err := json.Marshal(left)
	assistantRunMust(t, err)
	beforeRight, err := json.Marshal(right)
	assistantRunMust(t, err)
	if bytes.Equal(beforeLeft, beforeRight) {
		t.Fatal("fixture does not reproduce distinct typed RawMessage member order")
	}
	if !sameAssistantRunCheckpoint(left, right) || !sameAssistantRunCheckpoint(right, left) {
		t.Fatal("canonical object order changed checkpoint identity")
	}
	afterLeft, err := json.Marshal(left)
	assistantRunMust(t, err)
	afterRight, err := json.Marshal(right)
	assistantRunMust(t, err)
	if !bytes.Equal(beforeLeft, afterLeft) || !bytes.Equal(beforeRight, afterRight) {
		t.Fatal("comparison rewrote a retained checkpoint")
	}
}

func TestAssistantCheckpointEqualityPreservesNumericLexemes(t *testing.T) {
	for _, row := range []struct{ name, left, right string }{
		{"large-integer", "9007199254740993", "9007199254740992"},
		{"overflow-null", "1e400", "null"},
		{"overflow-exponent", "1e400", "1e399"},
		{"underflow", "1e-400", "0"},
		{"negative-zero", "-0", "0"},
		{"decimal-spelling", "10.50", "10.5"},
		{"numeric-string", "9007199254740993", `"9007199254740993"`},
		{"boolean-number", "false", "0"},
	} {
		for _, field := range []string{"context", "approval"} {
			t.Run(row.name+"/"+field, func(t *testing.T) {
				left, right := assistantCheckpointEqualityFixture(), assistantCheckpointEqualityFixture()
				set := func(cp *contract.Checkpoint, raw string) {
					if field == "context" {
						cp.Context.Entries[0].Value = json.RawMessage(raw)
					} else {
						cp.Approval.Args["value"] = json.RawMessage(raw)
					}
				}
				set(left, `{"z":`+row.left+`,"a":false}`)
				set(right, `{"a":false,"z":`+row.left+`}`)
				if !sameAssistantRunCheckpoint(left, right) {
					t.Fatal("same exact numeric token refused after object ordering")
				}
				set(right, `{"a":false,"z":`+row.right+`}`)
				if sameAssistantRunCheckpoint(left, right) || sameAssistantRunCheckpoint(right, left) {
					t.Fatal("distinct numeric token or type accepted")
				}
			})
		}
	}
}

func TestAssistantCheckpointEqualityPreservesWholeRecord(t *testing.T) {
	for _, row := range []struct {
		name   string
		change func(*contract.Checkpoint)
	}{
		{"missing-null", func(cp *contract.Checkpoint) {
			cp.Context.Entries[0].Value = json.RawMessage(`{"turns":[{"z":1,"a":2}],"questions":[],"missing":null}`)
		}},
		{"array-order", func(cp *contract.Checkpoint) {
			cp.Context.Entries[0].Value = json.RawMessage(`{"turns":[{"a":2,"z":1},null],"questions":[]}`)
		}},
		{"null-empty-array", func(cp *contract.Checkpoint) {
			cp.Context.Entries[0].Value = json.RawMessage(`{"turns":[{"z":1,"a":2}],"questions":null}`)
		}},
		{"request-number-spelling", func(cp *contract.Checkpoint) {
			changed := strings.Replace(*cp.Approval.Request, "10.50", "10.5", 1)
			cp.Approval.Request = &changed
		}},
		{"request-member-order", func(cp *contract.Checkpoint) {
			changed := `{"value":{"id":9007199254740993,"amount":10.50},"rehearsal":true}`
			cp.Approval.Request = &changed
		}},
		{"request-absent", func(cp *contract.Checkpoint) { cp.Approval.Request = nil }},
		{"valueJSON-member-order", func(cp *contract.Checkpoint) {
			changed := `{"questions":[],"turns":[{"a":2,"z":1}]}`
			cp.Context.Entries[0].ValueJSON = &changed
		}},
		{"valueJSON-absent", func(cp *contract.Checkpoint) { cp.Context.Entries[0].ValueJSON = nil }},
		{"entry-digest", func(cp *contract.Checkpoint) { cp.Context.Entries[0].Digest = strings.Repeat("d", 64) }},
		{"entry-bytes", func(cp *contract.Checkpoint) { cp.Context.Entries[0].Bytes++ }},
		{"checkpoint-digest", func(cp *contract.Checkpoint) { changed := "sha256:" + strings.Repeat("d", 64); cp.Digest = &changed }},
		{"checkpoint-digest-absent", func(cp *contract.Checkpoint) { cp.Digest = nil }},
		{"approval-state", func(cp *contract.Checkpoint) { cp.Approval.State = "approved" }},
	} {
		t.Run(row.name, func(t *testing.T) {
			left, right := assistantCheckpointEqualityFixture(), assistantCheckpointEqualityFixture()
			row.change(right)
			if sameAssistantRunCheckpoint(left, right) || sameAssistantRunCheckpoint(right, left) {
				t.Fatal("changed retained member accepted")
			}
		})
	}
	left, right := assistantCheckpointEqualityFixture(), assistantCheckpointEqualityFixture()
	left.Context.Entries[0].Value = json.RawMessage(`[null,false,0,"0"]`)
	right.Context.Entries[0].Value = json.RawMessage(`[false,null,0,"0"]`)
	if sameAssistantRunCheckpoint(left, right) {
		t.Fatal("array permutation accepted")
	}
}

func TestAssistantCheckpointEqualityRefusesAmbiguousAndMalformedRaw(t *testing.T) {
	for _, raw := range []string{`{"x":1,"x":1}`, `{"x":1,"\u0078":1}`, `{"outer":{"x":1,"x":2}}`} {
		cp := assistantCheckpointEqualityFixture()
		cp.Context.Entries[0].Value = json.RawMessage(raw)
		if _, err := json.Marshal(cp); err != nil {
			t.Fatal("duplicate fixture must reach the comparison validator", err)
		}
		if sameAssistantRunCheckpoint(cp, cp) {
			t.Fatal("ambiguous retained value accepted")
		}
	}
	cp := assistantCheckpointEqualityFixture()
	cp.Context.Entries[0].Value = json.RawMessage(`"\ud800"`)
	if sameAssistantRunCheckpoint(cp, cp) {
		t.Fatal("invalid Unicode scalar escape accepted")
	}
	for _, raw := range []string{`{"`, `[}`, `NaN`, `{"x":0}{"y":1}`} {
		bad, good := assistantCheckpointEqualityFixture(), assistantCheckpointEqualityFixture()
		bad.Approval.Args["value"] = json.RawMessage(raw)
		if _, err := json.Marshal(bad); err == nil {
			t.Fatal("malformed fixture did not cause a marshal error")
		}
		if sameAssistantRunCheckpoint(bad, bad) || sameAssistantRunCheckpoint(bad, good) || sameAssistantRunCheckpoint(good, bad) {
			t.Fatal("marshal failure accepted as checkpoint equality")
		}
	}
}

func TestAssistantCheckpointEqualityDepthMatchesStoredCheckpoint(t *testing.T) {
	cp := assistantCheckpointEqualityFixture()
	// A retained value is at depth four within a checkpoint; runs[].checkpoint
	// adds three levels in the authoritative persisted-ledger member validator.
	cp.Context.Entries[0].Value = json.RawMessage(strings.Repeat("[", 57) + "0" + strings.Repeat("]", 57))
	raw, err := json.Marshal(cp)
	assistantRunMust(t, err)
	ledger := append([]byte(`{"runs":[{"checkpoint":`), raw...)
	ledger = append(ledger, []byte(`}]}`)...)
	if err := assistantRunJSONMembers(ledger); err != nil {
		t.Fatal("boundary fixture exceeds the existing persisted-ledger limit", err)
	}
	if !sameAssistantRunCheckpoint(cp, cp) {
		t.Fatal("comparison refused checkpoint within authoritative storage depth")
	}
	cp.Context.Entries[0].Value = json.RawMessage(strings.Repeat("[", 60) + "0" + strings.Repeat("]", 60))
	if !sameAssistantRunCheckpoint(cp, cp) {
		t.Fatal("isolated checkpoint depth 64 refused")
	}
	cp.Context.Entries[0].Value = json.RawMessage(strings.Repeat("[", 61) + "0" + strings.Repeat("]", 61))
	if sameAssistantRunCheckpoint(cp, cp) {
		t.Fatal("isolated checkpoint depth above 64 accepted")
	}
}
