package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// These controls are saved on a real user request before canonical admission.
// An action is request data; only current host resolution can authorize it.
type assistantCreationAction struct {
	Kind            string `json:"kind"`
	CandidateDigest string `json:"candidateDigest,omitempty"`
	MessageID       string `json:"messageId,omitempty"`
	MessageDigest   string `json:"messageDigest,omitempty"`
	IssueID         string `json:"issueId,omitempty"`
	Token           string `json:"token,omitempty"`
	Choice          string `json:"choice,omitempty"`
}

type assistantCreationInput struct {
	Action      assistantCreationAction
	Scope       assistantCreationScope
	Candidate   string
	State       map[string]json.RawMessage
	Grounds     map[string]json.RawMessage
	Contract    json.RawMessage
	Fresh       func(context.Context) error
	FreshLocked func(context.Context) error
}

func readAssistantCreationAction(raw []byte) (*assistantCreationAction, error) {
	request, err := assistantTestObject(raw)
	if err != nil {
		return nil, err
	}
	if request["creation"] == nil {
		return nil, nil
	}
	fields, err := assistantTestControls(request["creation"], "kind", "candidateDigest", "messageId", "messageDigest", "issueId", "token", "choice")
	var a assistantCreationAction
	if err != nil || assistantHostArguments(request["creation"], &a) != nil {
		return nil, errAssistantRunInvalid
	}
	allowed := []string{"kind", "candidateDigest"}
	switch a.Kind {
	case "recover":
		allowed = []string{"kind", "messageId", "messageDigest"}
		if !assistantChatProjectionID(a.MessageID) || !assistantRunDigest(a.MessageDigest) {
			return nil, errAssistantRunInvalid
		}
	case "check", "retry-validation", "establish-cases":
	case "propose-correction", "approve-correction":
		allowed = append(allowed, "issueId")
		if !assistantRunText(a.IssueID, 128) {
			return nil, errAssistantRunInvalid
		}
		if a.Kind == "approve-correction" {
			allowed = append(allowed, "token")
			if !assistantRunDigest(a.Token) {
				return nil, errAssistantRunInvalid
			}
		}
	case "research-choice":
		allowed = []string{"kind", "choice"}
		if !contains([]string{"research", "provided"}, a.Choice) {
			return nil, errAssistantRunInvalid
		}
	default:
		return nil, errAssistantRunInvalid
	}
	for k := range fields {
		if !contains(allowed, k) {
			return nil, errAssistantRunInvalid
		}
	}
	if contains(allowed, "candidateDigest") && !assistantRunDigest(a.CandidateDigest) {
		return nil, errAssistantRunInvalid
	}
	return &a, nil
}

func compileAssistantCreationInput(source assistantRunSource) (*assistantCreationInput, error) {
	a, err := readAssistantCreationAction(source.Request)
	if err != nil || a == nil {
		return nil, err
	}
	if a.Kind == "research-choice" {
		request, err := assistantTestObject(source.Request)
		if err != nil || string(request["researchChoice"]) != string(assistantChatJSON(a.Choice)) {
			return nil, errAssistantRunScope
		}
	}
	chat, err := assistantTestObject(source.Original)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"pack", "graph", "input", "policy"} {
		if a.Kind == "research-choice" {
			break
		}
		if value := chat[key]; len(value) != 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errAssistantRunScope
		}
	}
	cp, err := assistantTestObject(chat["checkpoint"])
	if err != nil {
		return nil, err
	}
	state, err := assistantTestObject(cp["state"])
	if err != nil {
		return nil, err
	}
	candidates, err := assistantChatArray(state["candidates"])
	if err != nil {
		return nil, err
	}
	input := &assistantCreationInput{Action: *a, State: state, Grounds: map[string]json.RawMessage{}}
	if source.Binding.Phase != "work" && a.Kind != "check" && a.Kind != "research-choice" {
		return nil, errAssistantRunScope
	}
	input.Scope = assistantCreationScope{Principal: source.Binding.Principal, Session: source.Binding.Session, Project: source.Binding.Project, Chat: source.Binding.Owner.ID}
	if raw := chat["draftGeneration"]; raw != nil {
		if json.Unmarshal(raw, &input.Scope.Generation) != nil || input.Scope.Generation < 1 {
			return nil, errAssistantRunInvalid
		}
	}
	if a.Kind == "recover" {
		if len(candidates) != 0 {
			return nil, errAssistantRunScope
		}
	} else if a.Kind != "research-choice" {
		if len(candidates) == 0 {
			return nil, errAssistantRunScope
		}
		candidate, err := assistantTestObject(candidates[len(candidates)-1])
		if err != nil {
			return nil, err
		}
		input.Candidate, err = assistantTestString(candidate["text"])
		if err != nil || digestOf([]byte(input.Candidate)) != a.CandidateDigest {
			return nil, errAssistantRunScope
		}
		input.Scope.CandidateDigest = a.CandidateDigest
		pack, err := assistantTestObject([]byte(input.Candidate))
		if err != nil {
			return nil, err
		}
		contract := map[string]json.RawMessage{}
		for _, key := range []string{"outcomes", "evidenceRequirements", "escalation"} {
			if pack[key] != nil {
				contract[key] = pack[key]
			}
		}
		input.Contract = assistantChatEncodeObject(contract)
	}
	// Original person statements remain distinct from verified source text.
	input.Grounds, err = assistantCreationGrounds(state)
	if err != nil {
		return nil, err
	}
	basis := map[string]json.RawMessage{"candidate": assistantChatJSON(input.Candidate), "grounds": assistantChatEncodeObject(input.Grounds)}
	for _, key := range []string{"cases", "expectationIssues", "heldProposal", "unknowns"} {
		if state[key] != nil {
			basis[key] = state[key]
		}
	}
	for _, key := range []string{"draftId", "draftGeneration", "documents"} {
		if chat[key] != nil {
			basis[key] = chat[key]
		}
	}
	// Control projection may reorder JSON members. Canonicalize only this
	// comparison basis with number tokens intact; candidate bytes stay a string.
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(assistantChatEncodeObject(basis)))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil {
		return nil, errAssistantRunInvalid
	}
	input.Scope.SourceDigest = digestOf(assistantChatJSON(normalized))
	if input.Candidate != "" {
		citations, err := assistantCreationDeclaredCitations(input.Candidate)
		if err != nil {
			return nil, err
		}
		for _, citation := range citations {
			input.Grounds[citation.Location] = assistantChatJSON(citation.Quote)
		}
	}
	return input, nil
}

func assistantCreationGrounds(state map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	grounds := map[string]json.RawMessage{}
	inherited := state["inheritedStatements"]
	if inherited != nil {
		rows, err := assistantChatArray(inherited)
		if err != nil || len(rows) > 256 {
			return nil, errAssistantRunInvalid
		}
		for _, raw := range rows {
			var row []string
			if json.Unmarshal(raw, &row) != nil || len(row) != 2 || grounds[row[0]] != nil {
				return nil, errAssistantRunInvalid
			}
			grounds[row[0]] = assistantChatJSON(row[1])
		}
	}
	turns, err := assistantChatArray(state["turns"])
	if err != nil {
		return nil, err
	}
	userIndex := 0
	for _, raw := range turns {
		turn, err := assistantTestObject(raw)
		if err != nil {
			return nil, err
		}
		if string(turn["role"]) != `"user"` || string(turn["kind"]) == `"note"` {
			continue
		}
		userIndex++
		if turn["creation"] != nil {
			continue
		}
		id := "you-" + strconv.Itoa(userIndex)
		if inherited != nil {
			messageID, err := assistantTestString(turn["id"])
			if err != nil {
				return nil, err
			}
			id = "message:" + messageID
		}
		statement := turn["statement"]
		if statement == nil && turn["input"] == nil {
			statement = turn["text"]
		}
		if statement != nil {
			if _, err := assistantTestString(statement); err != nil {
				return nil, err
			}
			grounds[id] = bytes.Clone(statement)
		}
	}
	return grounds, nil
}

// Source resolution runs under the canonical private-data lock. Independently
// compare the linked artifact's lifecycle and retained case/source basis; the
// browser cannot authorize an old chat branch by claiming a generation.
func (s *Server) validateAssistantCreationDraftLocked(r *http.Request, root *os.Root, name string, chat, state map[string]json.RawMessage) error {
	draftID, err := assistantTestString(chat["draftId"])
	if chat["draftId"] == nil {
		return nil
	}
	if err != nil {
		return err
	}
	if raw := chat["pack"]; raw != nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	document, err := s.readWorkspaceRecord(root, draftPackName(name), true)
	if err != nil {
		return err
	}
	if err := s.authorizePolicyWorkspace(r, document.Content, true); err != nil {
		return err
	}
	fields, err := assistantTestObject(document.Content)
	if err != nil {
		return err
	}
	var deleted []string
	if fields["deleted"] != nil && json.Unmarshal(fields["deleted"], &deleted) != nil {
		return errAssistantRunInvalid
	}
	if contains(deleted, draftID) {
		return errAssistantRunScope
	}
	rows, err := assistantChatArray(fields["drafts"])
	if err != nil {
		return err
	}
	for _, raw := range rows {
		draft, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		if string(draft["id"]) != string(assistantChatJSON(draftID)) {
			continue
		}
		if !assistantHostArgumentsMatch(draft["generation"], chat["draftGeneration"]) || draft["finalized"] != nil && !bytes.Equal(bytes.TrimSpace(draft["finalized"]), []byte("null")) {
			return errAssistantRunScope
		}
		cp, err := assistantTestObject(draft["checkpoint"])
		if err != nil {
			return err
		}
		saved, err := assistantTestObject(cp["state"])
		if err != nil {
			return err
		}
		packText := func(state map[string]json.RawMessage) (string, error) {
			rows, err := assistantChatArray(state["candidates"])
			if err != nil || len(rows) == 0 {
				return "", errAssistantRunScope
			}
			last, err := assistantTestObject(rows[len(rows)-1])
			if err != nil {
				return "", err
			}
			return assistantTestString(last["text"])
		}
		a, err := packText(saved)
		if err != nil {
			return err
		}
		b, err := packText(state)
		if err != nil || a != b {
			return errAssistantRunScope
		}
		for _, key := range []string{"cases", "expectationIssues", "heldProposal", "unknowns"} {
			left, right := saved[key], state[key]
			fallback := []byte("[]")
			if key == "heldProposal" {
				fallback = []byte("null")
			}
			if left == nil {
				left = fallback
			}
			if right == nil {
				right = fallback
			}
			if !assistantHostArgumentsMatch(left, right) {
				return errAssistantRunScope
			}
		}
		originalGrounds, err := assistantCreationGrounds(saved)
		if err != nil {
			return err
		}
		currentGrounds, err := assistantCreationGrounds(state)
		if err != nil {
			return err
		}
		for id, original := range originalGrounds {
			if !assistantHostArgumentsMatch(original, currentGrounds[id]) {
				return errAssistantRunScope
			}
		}
		return nil
	}
	return errAssistantRunScope
}

func assistantCreationReviewerInput(input *assistantCreationInput) (*assistantExecutionInput, error) {
	data := map[string]any{"statements": json.RawMessage(assistantChatEncodeObject(input.Grounds)), "declarations": input.Contract}
	message := "Establish complete independent cases from the person's statements and verified source material only. Never infer expected decisions from draft rules or Runtime observations. Preserve missing facts, null, false, zero, numeric strings, evidence availability and handoff targets. Return one proposal document with cases: [{id, facts, evidenceAvailability?, expectedDisposition, expectedHandoffTarget?, expectationSource, rationale}]. expectationSource must name a supplied statement or verified citation. If policy does not establish an answer, retain unknowns instead of inventing it. Do not return a pack or alter existing cases."
	if input.Action.Kind == "propose-correction" {
		rows, err := assistantChatArray(input.State["expectationIssues"])
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			fields, err := assistantTestObject(raw)
			if err != nil {
				return nil, err
			}
			if string(fields["id"]) == string(assistantChatJSON(input.Action.IssueID)) && fields["resolved"] == nil {
				data["issue"] = json.RawMessage(raw)
			}
		}
		if data["issue"] == nil {
			return nil, errAssistantRunScope
		}
		message = "Review only the supplied invalid expectation using its independent source and Runtime finding. Preserve original case id, facts, source, evidence availability and exact expected handoff target. Never infer an expected answer from observed evaluation results or draft rules. Return one proposal document {expectedDisposition, rationale} only when independent evidence establishes the correction; otherwise return unknowns and no correction. The person must explicitly approve the visible proposal before the exact unchanged pack is retested."
	}
	value, err := runcontext.ParseJSON(assistantChatJSON(data))
	if err != nil {
		return nil, err
	}
	return &assistantExecutionInput{Request: runcontext.CurrentRequest{Message: message, Input: message + "\n\n" + value.Text()}, Documents: []runcontext.Document{{ID: "creation-case-basis", Kind: "artifact", Label: "Person statements and declared outputs; no draft rules or observed decisions", Value: value}}, CriticPurpose: "test-design", Critic: "off"}, nil
}
