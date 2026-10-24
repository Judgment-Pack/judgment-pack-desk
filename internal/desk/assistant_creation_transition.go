package desk

import (
	"context"
	"encoding/json"
)

func assistantCreationRetainedCall(run assistantRunRecord, id string) assistantCreationCall {
	return func(_ context.Context, name string, args any) (json.RawMessage, error) {
		op := assistantDialectOperation(run, id)
		if op == nil || op.Kind != "runtime" || op.Name != "tools/call" || op.State != "completed" {
			return nil, errAssistantRunInvalid
		}
		var frame struct {
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		var reply struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(op.Intent, &frame) != nil || frame.Params.Name != name || !assistantHostArgumentsMatch(frame.Params.Arguments, assistantChatJSON(args)) || json.Unmarshal(op.Result, &reply) != nil || reply.Error != nil || reply.Result == nil {
			return nil, errAssistantRunInvalid
		}
		return reply.Result, nil
	}
}

func assistantCreationRuntimePayload(run assistantRunRecord, before, id, name string, args any) (map[string]json.RawMessage, error) {
	if before != "" && !assistantDialectAfter(run, id, before) {
		return nil, errAssistantRunInvalid
	}
	raw, err := assistantCreationRetainedCall(run, id)(context.Background(), name, args)
	if err != nil {
		return nil, err
	}
	return assistantSavedPackPayload(raw)
}

func assistantCreationModelDocument(run assistantRunRecord) (map[string]json.RawMessage, error) {
	var document []byte
	for _, event := range run.Events {
		if event.Event.Proposal != nil {
			if document != nil {
				return nil, errAssistantRunInvalid
			}
			document = assistantChatJSON(event.Event.Proposal.Document)
		}
	}
	return assistantTestObject(document)
}

func validateAssistantCreationTransition(run assistantRunRecord, input *assistantCreationInput) error {
	r := run.Creation
	before, issues, err := assistantCreationCasesAndIssues(input)
	if err != nil {
		return err
	}
	same := func(a, b any) bool { return assistantHostArgumentsMatch(assistantChatJSON(a), assistantChatJSON(b)) }
	if r.Action != "recover" {
		unknowns := []string{}
		if raw := input.State["unknowns"]; raw != nil {
			if json.Unmarshal(raw, &unknowns) != nil || unknowns == nil {
				return errAssistantRunInvalid
			}
		}
		if r.Action == "establish-cases" {
			for _, event := range run.Events {
				if event.Event.Proposal != nil {
					unknowns = append(unknowns, event.Event.Proposal.Unknowns...)
				}
			}
		}
		if !same(unknowns, r.Unknowns) {
			return errAssistantRunScope
		}
	}
	switch r.Action {
	case "recover":
		if len(r.Cases) != 0 || len(r.Issues) != 0 || len(r.Held) != 0 || r.Proposal != nil || r.Approved != nil {
			return errAssistantRunScope
		}
	case "check":
		if !same(before, r.Cases) || len(r.Issues) != len(issues) || len(r.Held) != 0 || r.Proposal != nil || r.Approved != nil {
			return errAssistantRunScope
		}
		inputs := append([]assistantCreationCase{}, before...)
		for _, issue := range issues {
			inputs = append(inputs, issue.Original)
		}
		if len(inputs) > 0 {
			findings, err := validateAssistantCreationExpectations(context.Background(), inputs, assistantCreationRetainedCall(run, r.ExpectationID))
			if err != nil {
				return err
			}
			for _, finding := range findings[:len(before)] {
				if finding.Status != "valid" {
					return errAssistantRunScope
				}
			}
			for i, issue := range issues {
				finding := findings[len(before)+i]
				if finding.Status != "invalid" || !same(r.Issues[i].Position, issue.Position) || !same(r.Issues[i].Original, issue.Original) || !same(r.Issues[i].Finding, finding) {
					return errAssistantRunScope
				}
			}
		}
		if len(issues) > 0 && r.Check != nil {
			return errAssistantRunScope
		}
	case "establish-cases", "retry-validation":
		if len(before) != 0 || len(issues) != 0 || r.Proposal != nil || r.Approved != nil {
			return errAssistantRunScope
		}
		var raw []byte
		if r.Action == "establish-cases" {
			document, err := assistantCreationModelDocument(run)
			if err != nil {
				return err
			}
			raw = document["cases"]
		} else {
			held, err := assistantTestObject(input.State["heldProposal"])
			if err != nil {
				return err
			}
			raw = held["admitted"]
		}
		proposed, err := readAssistantCreationCases(raw, input.Grounds)
		if err != nil || len(proposed) == 0 {
			return errAssistantRunScope
		}
		if len(r.Held) > 0 {
			if !same(proposed, r.Held) || len(r.Cases) != 0 || len(r.Issues) != 0 || r.Check != nil {
				return errAssistantRunScope
			}
			return nil
		}
		findings, err := validateAssistantCreationExpectations(context.Background(), proposed, assistantCreationRetainedCall(run, r.ExpectationID))
		if err != nil {
			return err
		}
		cases := []assistantCreationCase{}
		expectedIssues := []assistantCreationIssue{}
		for i, row := range proposed {
			if findings[i].Status == "invalid" {
				// Earlier retained receipts predate explicit position custody.
				// Validate a supplied slot exactly; omission remains legacy data.
				position := &i
				if len(expectedIssues) < len(r.Issues) && r.Issues[len(expectedIssues)].Position == nil {
					position = nil
				}
				expectedIssues = append(expectedIssues, assistantCreationIssue{Position: position, ID: row.ID, Original: row, Finding: findings[i]})
			} else {
				row.ExpectedDisposition = []byte(findings[i].Canonical)
				cases = append(cases, row)
			}
		}
		if !same(cases, r.Cases) || !same(expectedIssues, r.Issues) {
			return errAssistantRunScope
		}
	case "propose-correction":
		if !same(before, r.Cases) || !same(issues, r.Issues) || len(r.Held) != 0 || r.Check != nil || r.Approved != nil || r.Proposal == nil {
			return errAssistantRunScope
		}
		var issue *assistantCreationIssue
		for i := range issues {
			if issues[i].ID == input.Action.IssueID {
				issue = &issues[i]
			}
		}
		if issue == nil || !same(issue.Original, r.Proposal.Original) {
			return errAssistantRunScope
		}
		freshFinding, err := validateAssistantCreationExpectations(context.Background(), []assistantCreationCase{issue.Original}, assistantCreationRetainedCall(run, r.IssueValidationID))
		if err != nil || !same(freshFinding[0], issue.Finding) || !freshFinding[0].Admitted && !freshFinding[0].Unreachable {
			return errAssistantRunScope
		}
		document, err := assistantCreationModelDocument(run)
		if err != nil {
			return err
		}
		rationale, err := assistantTestString(document["rationale"])
		if err != nil || rationale != r.Proposal.Rationale {
			return errAssistantRunScope
		}
		replacement := issue.Original
		replacement.ExpectedDisposition = document["expectedDisposition"]
		findings, err := validateAssistantCreationExpectations(context.Background(), []assistantCreationCase{replacement}, assistantCreationRetainedCall(run, r.ExpectationID))
		if err != nil || findings[0].Status != "valid" || !assistantHostArgumentsMatch([]byte(findings[0].Canonical), r.Proposal.ExpectedDisposition) {
			return errAssistantRunScope
		}
	case "approve-correction":
		if r.Proposal != nil && r.Approved == nil && r.Check == nil {
			if !same(before, r.Cases) || !same(issues, r.Issues) || len(r.Held) != 0 || r.Proposal.Token != input.Action.Token || r.Proposal.Scope != input.Scope {
				return errAssistantRunScope
			}
			matched := false
			for _, issue := range issues {
				if issue.ID == input.Action.IssueID && same(issue.Original, r.Proposal.Original) {
					matched = true
				}
			}
			if !matched {
				return errAssistantRunScope
			}
			return nil
		}
		if r.Approved == nil || r.Proposal != nil || r.Check == nil || len(r.Held) != 0 || !assistantCreationSuccessfulCheck(*r.Check, input.Scope.CandidateDigest, r.Cases) {
			return errAssistantRunScope
		}
		var issue *assistantCreationIssue
		remaining := []assistantCreationIssue{}
		for i := range issues {
			if issues[i].ID == input.Action.IssueID {
				issue = &issues[i]
			} else {
				remaining = append(remaining, issues[i])
			}
		}
		if issue == nil {
			return errAssistantRunScope
		}
		issue.Proposal = r.Approved
		replacement, err := assistantCreationCorrectedCase(input.Scope, *issue, *r.Approved, input.Action.Token, before)
		if err != nil {
			return err
		}
		findings, err := validateAssistantCreationExpectations(context.Background(), []assistantCreationCase{replacement}, assistantCreationRetainedCall(run, r.ExpectationID))
		if err != nil || findings[0].Status != "valid" {
			return errAssistantRunScope
		}
		staged, err := assistantCreationStage(before, issues, *issue, replacement)
		if err != nil || !same(staged, r.Cases) || !same(remaining, r.Issues) {
			return errAssistantRunScope
		}
	default:
		return errAssistantRunInvalid
	}
	return nil
}
