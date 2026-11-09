package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const projectionValidUnicode = `{"nested":{"\ud83d\ude42":"café","value":"\ud83d\ude42","literal":"\\ud800","replacement":"�"}}`
const projectionExistingUnicode = ` { "Existing":"café 🙂", "pair":"\ud83d\ude42", "literal":"\\ud800" } `

func projectionUnicodeCheckpoint(t *testing.T, f *chatProjectionFixture, run assistantRunRecord, args, result json.RawMessage) assistantRunRecord {
	t.Helper()
	_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
		held, err := assistantChatProjectionRun(l, run.ID)
		if err != nil {
			return false, err
		}
		held.Checkpoint.Operations = append(held.Checkpoint.Operations, contract.CheckpointOperation{ID: "3f71c7b0-7df4-4eb6-96f3-f3a1390dc3a0", Name: "read_unicode", Args: args, Status: "complete", Result: &contract.ToolResultMCPToolResult{StructuredContent: result}})
		held.Revision++
		return true, nil
	})
	assistantRunMust(t, err)
	run, err = f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	return run
}
func projectionUnicodeOriginals(run assistantRunRecord) []byte {
	run.ChatProjection = nil
	run.ProjectionIssue = ""
	run.Revision = 0
	raw, _ := json.Marshal(run)
	return raw
}
func exportProjectionUnicode(t *testing.T, name string, raw []byte) {
	t.Helper()
	if dir := os.Getenv("JPS_TEST_PROJECTION_UNICODE_OUTPUTS"); dir != "" {
		assistantRunMust(t, os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600))
	}
}
func TestAssistantChatProjectionUnicodeRefusal(t *testing.T) {
	for _, fixture := range []struct{ name, kind, payload string }{
		{"proposal-review-high", "proposal", `{"Text":"\ud800"}`},
		{"proposal-nested-high", "proposal", `{"other":[{"value":"\ud800"}]}`},
		{"proposal-key-low", "proposal", `{"\udc00":true}`},
		{"checkpoint-args-high", "args", `{"payload":{"value":"\ud800"}}`},
		{"checkpoint-result-low", "result", `{"values":["\udc00"]}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			f := newChatProjectionFixture(t)
			events := []contract.RunEvent{projectionMessage("Retained Unicode response")}
			if fixture.kind == "proposal" {
				events = append(events, contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: map[string]json.RawMessage{"Payload": json.RawMessage(fixture.payload)}, Unknowns: []string{}}})
			}
			run := f.admit(t, "chat-one", "message-one", fixture.name, "complete", events...)
			if fixture.kind == "args" {
				run = projectionUnicodeCheckpoint(t, f, run, json.RawMessage(fixture.payload), json.RawMessage(`{"value":false}`))
			}
			if fixture.kind == "result" {
				run = projectionUnicodeCheckpoint(t, f, run, json.RawMessage(`{"value":null}`), json.RawMessage(fixture.payload))
			}
			before := f.read(t)
			originals := projectionUnicodeOriginals(run)
			err := f.s.reconcileAssistantChatProjection(f.request(), run.ID)
			observed := f.read(t)
			// Observation export also runs under the deliberate omission probe. Its
			// browser reader must then observe the actual unsafe saved output.
			exportProjectionUnicode(t, fixture.name, observed)
			if !errors.Is(err, errAssistantChatProjectionUnsupported) {
				t.Fatal("unsupported Unicode reached prepared/write/ack", err)
			}
			saved, e := f.store.Read(t.Context(), run.Binding, run.ID)
			assistantRunMust(t, e)
			if saved.ProjectionIssue != "unsupported-unicode" || saved.ChatProjection != nil || !bytes.Equal(before, observed) || !bytes.Equal(originals, projectionUnicodeOriginals(saved)) {
				t.Fatal("Unicode refusal changed source or retained originals")
			}
			revision := saved.Revision
			if err = f.s.prepareAssistantChatProjection(f.request(), run.ID); !errors.Is(err, errAssistantChatProjectionUnsupported) {
				t.Fatal("repeated Unicode limitation changed", err)
			}
			saved, e = f.store.Read(t.Context(), run.Binding, run.ID)
			assistantRunMust(t, e)
			if saved.Revision != revision || !bytes.Equal(originals, projectionUnicodeOriginals(saved)) {
				t.Fatal("Unicode refusal is not durable/idempotent")
			}
		})
	}
}
func TestAssistantChatProjectionValidUnicode(t *testing.T) {
	f := newChatProjectionFixture(t)
	fields, rows, err := assistantChatRows(f.read(t))
	assistantRunMust(t, err)
	chat, err := assistantChatObject(rows[0])
	assistantRunMust(t, err)
	cp, err := assistantChatObject(chat["checkpoint"])
	assistantRunMust(t, err)
	state, err := assistantChatObject(cp["state"])
	assistantRunMust(t, err)
	candidates, err := assistantChatArray(state["candidates"])
	assistantRunMust(t, err)
	candidates = append(candidates, assistantChatJSON(map[string]any{"text": projectionExistingUnicode}))
	state["candidates"] = assistantChatEncodeArray(candidates)
	cp["state"] = assistantChatEncodeObject(state)
	chat["checkpoint"] = assistantChatEncodeObject(cp)
	rows[0] = assistantChatEncodeObject(chat)
	fields["chats"] = assistantChatEncodeArray(rows)
	f.save(t, assistantChatEncodeObject(fields))
	event := contract.RunEvent{Proposal: &contract.RunEventProposal{Type: "proposal", Document: map[string]json.RawMessage{"Payload": json.RawMessage(projectionValidUnicode)}, Unknowns: []string{}}}
	run := f.admit(t, "chat-one", "message-one", "valid-unicode", "complete", event, projectionMessage("café 🙂 �"))
	run = projectionUnicodeCheckpoint(t, f, run, json.RawMessage(projectionValidUnicode), json.RawMessage(projectionValidUnicode))
	originals := projectionUnicodeOriginals(run)
	assistantRunMust(t, f.s.reconcileAssistantChatProjection(f.request(), run.ID))
	saved, err := f.store.Read(t.Context(), run.Binding, run.ID)
	assistantRunMust(t, err)
	if saved.ChatProjection == nil || !saved.ChatProjection.Acknowledged || !bytes.Equal(originals, projectionUnicodeOriginals(saved)) {
		t.Fatal("valid Unicode changed retained run")
	}
	raw := f.read(t)
	if !bytes.Contains(raw, []byte("café")) || !strings.Contains(string(saved.ChatProjection.Output), `\\ud83d\\ude42`) {
		t.Fatal("valid Unicode or escaped pair was changed")
	}
	exportProjectionUnicode(t, "valid-unicode", raw)
}
