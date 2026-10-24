package desk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAssistantCreationLegacyIdentityCustody(t *testing.T) {
	legacy := json.RawMessage(`{"role":"assistant","kind":"message","text":"original","n":1.20,"large":9007199254740993}`)
	request := json.RawMessage(`{"id":"current","role":"user","kind":"message","text":"Recover the original"}`)
	chat := func(turns ...json.RawMessage) []byte {
		return assistantChatJSON(map[string]any{"checkpoint": map[string]any{"state": map[string]any{"turns": turns}}})
	}
	original := chat(legacy, request)
	before := append([]byte(nil), original...)
	if !assistantChatRequest(original, "current", digestOf(request)) || !bytes.Equal(before, original) || !bytes.Contains(original, []byte(`"n":1.20`)) || !bytes.Contains(original, []byte(`9007199254740993`)) {
		t.Fatal("legacy identity read changed original bytes or refused current request")
	}
	collision := chat(legacy, json.RawMessage(`{"id":"legacy-turn-0","role":"assistant"}`), request)
	if assistantChatRequest(collision, "current", digestOf(request)) {
		t.Fatal("explicit ID collided with a legacy association alias")
	}
	if assistantChatRequest(chat(json.RawMessage(`{"role":"user"}`)), "legacy-turn-0", digestOf([]byte(`{"role":"user"}`))) {
		t.Fatal("historical alias became a current request identity")
	}
	run := assistantRunRecord{Binding: assistantRunBinding{Owner: assistantRunOwner{Revision: digestOf(original)}}}
	for _, changed := range [][]byte{chat(json.RawMessage(`{"role":"assistant","kind":"message","text":"replacement"}`), request), chat(json.RawMessage(`{"role":"assistant","text":"inserted"}`), legacy, request), bytes.Replace(original, []byte("1.20"), []byte("1.2"), 1)} {
		if assistantChatProjectionMatches(&run, changed) {
			t.Fatal("changed, inserted or numerically rewritten source retargeted the captured message")
		}
	}
}

func TestAssistantCreationCurrentSourceProof(t *testing.T) {
	raw, pin, ref := assistantDocumentFixture(t, "web", "http", nil)
	s, _, e, u, _ := assistantChatDocumentFixture(t, "api", "normal", raw, pin, []assistantDocumentReference{ref})
	location := "attachment:" + ref.ID + "/" + ref.Digest + "/page/1"
	text := string(assistantChatJSON(map[string]any{"sources": []any{map[string]any{"id": "source-one", "locator": map[string]string{"value": "https://example.com/report"}, "citation": map[string]string{"location": location, "excerpt": "Selected first page."}}}}))
	input := &assistantCreationInput{Candidate: text}
	citations, proofs, err := e.verifyAssistantCreationSources(t.Context(), input)
	assistantRunMust(t, err)
	if len(citations) != 1 || !citations[0].Traced || len(proofs) != 1 || len(u.arrivals()) != 0 || !assistantCreationProofsMatch(input, citations, proofs, input.Scope.EvidenceDigest) {
		t.Fatal("current rooted proof missing or model dispatched")
	}
	if err := input.Fresh(t.Context()); err != nil {
		t.Fatal("fresh proof refused", err)
	}
	for _, replace := range [][2]string{{"Selected first page.", "Invented quote"}, {"https://example.com/report", "https://foreign.example/"}, {"/page/1", "/page/9"}} {
		changed := &assistantCreationInput{Candidate: strings.Replace(text, replace[0], replace[1], 1)}
		if _, _, err := e.verifyAssistantCreationSources(t.Context(), changed); err == nil {
			t.Fatal("unbound source admitted", replace[0])
		}
	}
	// A cache only avoids repeating cryptographic work; current rooted bytes
	// are still checked at every finite call and final projection transaction.
	assistantChatStoreDocument(t, s, ref, append(raw, ' '))
	if input.Fresh(t.Context()) == nil {
		t.Fatal("changed retained source reused cached verification")
	}
}

func TestAssistantCreationRetainedFlagsDoNotAuthorizeLegacyProof(t *testing.T) {
	text := `{"sources":[{"id":"old","locator":{"value":"https://example.com/"},"citation":{"location":"src-1#e1","excerpt":"Old retained quote"}}],"verification":{"state":"verified"},"approved":true}`
	if _, err := assistantCreationDeclaredCitations(text); err == nil {
		t.Fatal("historical source flag became current proof")
	}
}

func TestAssistantCreationRecoveryIsStrictAndNumberPreserving(t *testing.T) {
	text := "```json\n" + `{"proposal":{"kind":"create","document":{"specVersion":"0.2.0-draft","id":"one","title":"Retained","n":1.20,"flag":false,"nil":null},"unknowns":[]}}` + "\n```"
	document, unknowns, err := recoverAssistantCreationText(text)
	if err != nil || !strings.Contains(document, `"n":1.20`) || unknowns == nil {
		t.Fatal("exact retained proposal lost", err)
	}
	for _, bad := range []string{text + "\n" + text, strings.TrimSuffix(text, "```"), strings.Replace(text, `"unknowns":[]`, `"unknowns":null`, 1), strings.Replace(text, `"kind":"create"`, `"kind":"replace"`, 1), text + "\n```json\n{\"proposal\":\n```"} {
		if _, _, err := recoverAssistantCreationText(bad); err == nil {
			t.Fatal("partial or ambiguous proposal recovered")
		}
	}
	var pack map[string]json.RawMessage
	assistantRunMust(t, json.Unmarshal([]byte(document), &pack))
	if string(pack["n"]) != "1.20" {
		t.Fatal("numeric token coerced")
	}
}
