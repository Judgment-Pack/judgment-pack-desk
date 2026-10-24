package desk

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAssistantCreationDraftCurrentCustody(t *testing.T) {
	for _, mutation := range []string{"current", "generation", "candidate", "facts", "target", "statement", "held", "unknowns", "finalized", "deleted"} {
		t.Run(mutation, func(t *testing.T) {
			s, token, start, _ := assistantProtocolFixture(t, assistantProtocolComplete)
			saved := policyWorkspaceRead(t, s, token, "/api/conversations")
			fields, chats, err := assistantChatRows(saved.Content)
			assistantRunMust(t, err)
			chat, err := assistantTestObject(chats[0])
			assistantRunMust(t, err)
			state := map[string]any{"turns": []any{map[string]any{"id": "person", "role": "user", "kind": "brief", "text": "Always accept without a handoff.", "at": "2026-10-23T00:00:00Z"}, map[string]any{"id": start.RequestID, "role": "user", "kind": "message", "text": "Check", "creation": assistantCreationAction{Kind: "check", CandidateDigest: digestOf([]byte(reviewPack))}, "at": "2026-10-23T00:01:00Z"}}, "candidates": []any{map[string]string{"text": reviewPack}}, "cases": []any{map[string]any{"id": "one", "facts": map[string]any{"zero": 0, "false": false, "nil": nil}, "expectedDisposition": json.RawMessage(creationDisposition), "expectedHandoffTarget": nil, "expectationSource": "you-1", "rationale": "The person requires accept."}}, "expectationIssues": []any{}, "heldProposal": nil, "unknowns": []string{}}
			cp := map[string]any{"sources": []any{}, "state": state}
			draft := map[string]any{"id": "draft-guard", "generation": 1, "title": "Guard fixture", "folderId": "home", "mode": "draft", "createdAt": "2026-10-23T00:00:00Z", "updatedAt": "2026-10-23T00:01:00Z", "documents": []any{}, "checkpoint": json.RawMessage(assistantChatJSON(cp))}
			deleted := []string{}
			switch mutation {
			case "finalized":
				draft["finalized"] = map[string]string{"id": "saved", "path": "one.json", "digest": "current"}
			case "deleted":
				deleted = []string{"draft-guard"}
			}
			drafts := []any{draft}
			if mutation == "deleted" {
				drafts = []any{}
			}
			policyWorkspaceSave(t, s, token, "/api/draft-packs", map[string]any{"version": 1, "drafts": drafts, "deleted": deleted})
			chat["draftId"] = assistantChatJSON("draft-guard")
			chat["draftGeneration"] = []byte("1")
			switch mutation {
			case "generation":
				chat["draftGeneration"] = []byte("2")
			case "candidate":
				state["candidates"] = []any{map[string]string{"text": reviewPack + " "}}
				state["turns"].([]any)[1].(map[string]any)["creation"] = assistantCreationAction{Kind: "check", CandidateDigest: digestOf([]byte(reviewPack + " "))}
			case "facts":
				state["cases"].([]any)[0].(map[string]any)["facts"] = map[string]any{"zero": "0"}
			case "target":
				state["cases"].([]any)[0].(map[string]any)["expectedHandoffTarget"] = "other"
			case "statement":
				state["turns"].([]any)[0].(map[string]any)["text"] = "Always reject."
			case "held":
				state["heldProposal"] = map[string]any{"candidateDigest": digestOf([]byte(reviewPack)), "admitted": state["cases"], "dropped": []any{}, "unknowns": []any{}}
			case "unknowns":
				state["unknowns"] = []string{"A policy choice changed."}
			}
			chat["checkpoint"] = assistantChatJSON(cp)
			chats[0] = assistantChatEncodeObject(chat)
			fields["chats"] = assistantChatEncodeArray(chats)
			w := policyWorkspaceCall(s, token, "PUT", "/api/conversations", saved.SHA256, json.RawMessage(assistantChatEncodeObject(fields)))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			retained := policyWorkspaceRead(t, s, token, "/api/conversations")
			_, chats, err = assistantChatRows(retained.Content)
			assistantRunMust(t, err)
			start.Revision = digestOf(chats[0])
			r := httptest.NewRequest("POST", "/api/assistant-runs", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			identity, _, err := s.assistantRunIdentity(r)
			assistantRunMust(t, err)
			_, err = s.readAssistantRunSource(r, start, identity, s.assistantRuns().project)
			if (err == nil) != (mutation == "current") {
				t.Fatal("draft source admitted a changed lifecycle or evidence basis", mutation, err)
			}
			if len(assistantAuthoringLedger(t, s).Runs) != 0 {
				t.Fatal("readiness check admitted a model")
			}
		})
	}
}
