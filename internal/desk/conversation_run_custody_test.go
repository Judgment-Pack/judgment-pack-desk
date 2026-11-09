package desk

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func TestConversationExactRows(t *testing.T) {
	old := []byte(`{"version":1,"chats":[{ "id":"one", "n":9007199254740993,"text":"\u0061" },{"id":"two","n":1}]}`)
	incoming := []byte(`{"version":1,"chats":[{"text":"a","n":9007199254740993,"id":"one"},{"id":"two","n":2}]}`)
	got, err := preserveConversationRows(old, incoming)
	assistantRunMust(t, err)
	_, rows, err := assistantChatRows(got)
	assistantRunMust(t, err)
	if string(rows[0]) != `{ "id":"one", "n":9007199254740993,"text":"\u0061" }` {
		t.Fatal("raw row rewritten", string(rows[0]))
	}
	for _, value := range []string{"9007199254740992", "9007199254740993.0", "1", "1.0"} {
		changed := strings.Replace(string(incoming), "9007199254740993", value, 1)
		got, err = preserveConversationRows(old, []byte(changed))
		assistantRunMust(t, err)
		_, rows, err = assistantChatRows(got)
		assistantRunMust(t, err)
		if !strings.Contains(string(rows[0]), `"n":`+value) {
			t.Fatal("number edit hidden", string(rows[0]))
		}
	}
	for _, bad := range []string{`{"version":1,"chats":[{"id":"one","id":"two"}]}`, `{"version":1,"chats":[{"id":"one"},{"id":"one"}]}`, `{"version":1,"chats":[{"id":1}]}`} {
		if _, err = preserveConversationRows(old, []byte(bad)); err == nil {
			t.Fatal("malformed rows accepted", bad)
		}
	}
}
func TestConversationUnrelatedCASRetainsStartedRun(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s, token, start, executor := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := e.Event(ctx, "message", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Continued after unrelated chat CAS"}}); err != nil {
			return err
		}
		return assistantProtocolComplete(ctx, e)
	})
	admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	<-started
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	if saved.ChatRevisions[start.OwnerID] != start.Revision || admitted.AdmissionID != start.AdmissionID {
		t.Fatal("raw revision/admission custody missing")
	}
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(saved.Content, &doc))
	original := string(doc.Chats[0])
	var fields map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &fields))
	// Reordering/escaping from a browser is structurally unchanged; a different chat is new.
	doc.Chats[0] = assistantChatEncodeObject(fields)
	doc.Chats = append(doc.Chats, json.RawMessage(`{"id":"unrelated","title":"Local draft"}`))
	update := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, doc)
	if update.Code != 200 {
		t.Fatal(update.Code, update.Body.String())
	}
	current := policyWorkspaceRead(t, s, token, "/api/conversations")
	assistantRunMust(t, json.Unmarshal(current.Content, &doc))
	if string(doc.Chats[0]) != original || current.ChatRevisions[start.OwnerID] != start.Revision {
		t.Fatal("unrelated CAS rewrote pinned source")
	}
	close(release)
	complete := assistantProtocolWait(t, s, token, admitted.ID, "complete")
	if complete.AdmissionID != start.AdmissionID || executor.calls.Load() != 1 || len(complete.Events) != 1 {
		t.Fatal("continuation custody/replay", complete)
	}
	assistantRunClientJSON(t, "custody-terminal.json", complete)
}

// Actual authenticated HTTP output for browser controller + store journeys.
// Expectations are handwritten in the browser test; these are ephemeral observations.
func TestSavedChatConsumerProducer(t *testing.T) {
	s, token, start, _ := assistantProtocolFixture(t, func(ctx context.Context, e *assistantRunExecution) error {
		if err := e.Event(ctx, "final-prose", projectionMessage("Service-owned terminal response")); err != nil {
			return err
		}
		if err := assistantProtocolComplete(ctx, e); err != nil {
			return err
		}
		if err := e.writer.State(ctx, "complete"); err != nil {
			return err
		}
		return e.ProjectChat(ctx)
	})
	raw, err := os.ReadFile("testdata/assistant-chat-projection-source.json")
	assistantRunMust(t, err)
	var doc conversationDocument
	assistantRunMust(t, json.Unmarshal(raw, &doc))
	var chat map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(doc.Chats[0], &chat))
	var cp map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(chat["checkpoint"], &cp))
	var state map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal(cp["state"], &state))
	state["candidates"] = json.RawMessage(`[]`)
	cp["state"] = assistantChatEncodeObject(state)
	chat["checkpoint"] = assistantChatEncodeObject(cp)
	chat["mode"] = json.RawMessage(`"draft"`)
	chat["serverRequest"] = json.RawMessage(`{"requestId":"message-one","admissionId":"admission-one"}`)
	doc.Chats = []json.RawMessage{assistantChatEncodeObject(chat)}
	before := policyWorkspaceRead(t, s, token, "/api/conversations")
	reply := policyWorkspaceCall(s, token, "PUT", "/api/conversations", before.SHA256, doc)
	if reply.Code != 200 {
		t.Fatal(reply.Code, reply.Body.String())
	}
	saved := policyWorkspaceRead(t, s, token, "/api/conversations")
	start.Revision = saved.ChatRevisions[start.OwnerID]
	assistantRunClientJSON(t, "consumer-source.json", saved)
	admitted := assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs", start), 201)
	complete := assistantProtocolWait(t, s, token, admitted.ID, "complete")
	if complete.ChatProjection == nil || !complete.ChatProjection.Acknowledged {
		t.Fatal("projection acknowledgement missing")
	}
	assistantRunClientJSON(t, "consumer-terminal.json", complete)
	assistantRunClientJSON(t, "consumer-conversations.json", policyWorkspaceRead(t, s, token, "/api/conversations"))
}

func TestSavedChatConsumerApprovalVector(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/assistant/serverRuns/fixtures/approval.json")
	assistantRunMust(t, err)
	var expected assistantRunView
	assistantRunMust(t, json.Unmarshal(raw, &expected))
	run := assistantRunRecord{ID: expected.ID, AdmissionID: expected.AdmissionID, State: expected.State, Checkpoint: expected.Checkpoint}
	actual := assistantRunPublic(run)
	if actual.AdmissionID != "admission-one" || actual.ApprovalReference == nil || expected.ApprovalReference == nil || *actual.ApprovalReference != *expected.ApprovalReference {
		t.Fatal("approval vector must bind exact server reference")
	}
	for _, state := range []string{"running", "complete", "awaiting-client", "stopped"} {
		run.State = state
		if assistantRunPublic(run).ApprovalReference != nil {
			t.Fatal("stale reference", state)
		}
	}
	run.State = "awaiting-input"
	run.Checkpoint.Approval.State = "approved"
	if assistantRunPublic(run).ApprovalReference != nil {
		t.Fatal("resolved approval revived")
	}
}

func TestSavedChatConsumerCheckVector(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/assistant/serverRuns/fixtures/pack-checks.json")
	assistantRunMust(t, err)
	var expected assistantRunView
	assistantRunMust(t, json.Unmarshal(raw, &expected))
	if len(expected.PackChecks) != 1 || expected.PackChecks[0].Valid {
		t.Fatal("invalid check fixture missing")
	}
	check := expected.PackChecks[0]
	prior := check
	prior.Valid = true
	run := assistantRunRecord{PackChecks: []assistantPackRecheck{{DocumentDigest: check.DocumentDigest, Check: &prior}, {DocumentDigest: check.DocumentDigest, Check: &check}}}
	actual := assistantRunPublic(run)
	if len(actual.PackChecks) != 1 || actual.PackChecks[0].Valid || actual.PackChecks[0].DocumentDigest != check.DocumentDigest || actual.PackChecks[0].SuiteKey != "[]" {
		t.Fatal("latest failed check must replace older pass")
	}
	run.PackChecks = append(run.PackChecks, assistantPackRecheck{DocumentDigest: check.DocumentDigest})
	if assistantRunPublic(run).PackChecks[0].Valid {
		t.Fatal("incomplete receipt hid completed failure")
	}
	run.PackChecks = append(run.PackChecks, assistantPackRecheck{DocumentDigest: check.DocumentDigest, Check: &prior})
	if !assistantRunPublic(run).PackChecks[0].Valid {
		t.Fatal("latest completed pass missing")
	}
}
