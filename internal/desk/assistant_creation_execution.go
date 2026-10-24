package desk

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

type assistantCreationReceipt struct {
	HeldWitness       string                         `json:"heldWitness,omitempty"`
	ProposalWitness   string                         `json:"proposalWitness,omitempty"`
	ValidationID      string                         `json:"validationId,omitempty"`
	TestID            string                         `json:"testId,omitempty"`
	ExpectationID     string                         `json:"expectationId,omitempty"`
	IssueValidationID string                         `json:"issueValidationId,omitempty"`
	Source            []byte                         `json:"source"`
	Request           []byte                         `json:"request"`
	Action            string                         `json:"action"`
	Scope             assistantCreationScope         `json:"scope"`
	Candidate         string                         `json:"candidate"`
	Unknowns          []string                       `json:"unknowns"`
	Cases             []assistantCreationCase        `json:"cases"`
	Held              []assistantCreationCase        `json:"held,omitempty"`
	Issues            []assistantCreationIssue       `json:"issues"`
	Check             *assistantPackCheck            `json:"check,omitempty"`
	Proposal          *assistantCreationCorrection   `json:"proposal,omitempty"`
	Approved          *assistantCreationCorrection   `json:"approved,omitempty"`
	Citations         []assistantCreationCitation    `json:"citations"`
	Sources           []assistantCreationSourceProof `json:"sources"`
}

func assistantCreationSuite(cases []assistantCreationCase) []byte {
	rows := []json.RawMessage{}
	for _, row := range cases {
		fields, _ := assistantTestObject(assistantChatJSON(row))
		delete(fields, "expectationSource")
		delete(fields, "rationale")
		rows = append(rows, assistantChatJSON(map[string]any{"id": row.ID, "row": json.RawMessage(assistantChatEncodeObject(fields)), "revision": 0, "name": row.ID, "rationale": row.Rationale, "origin": "ai", "sources": []any{}, "sourceMappings": map[string]any{}}))
	}
	return assistantChatJSON(map[string]any{"revision": 0, "cases": rows, "deleted": []string{}, "runs": []any{}, "recovered": []any{}})
}

func (e *assistantRunExecution) retainAssistantCreation(ctx context.Context, input *assistantCreationInput, receipt assistantCreationReceipt) error {
	if input.Fresh != nil {
		if err := input.Fresh(ctx); err != nil {
			return err
		}
	}
	err := e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if assistantRunTerminal(run.State) || run.Historical {
			return false, errAssistantRunDispatch
		}
		if input.FreshLocked != nil {
			if err := input.FreshLocked(ctx); err != nil {
				return false, err
			}
		}
		run.Creation = &receipt
		return true, nil
	})
	e.changed(err)
	return err
}

func (e *assistantRunExecution) checkAssistantCreation(ctx context.Context, input *assistantCreationInput, cases []assistantCreationCase, call assistantCreationCall) (assistantPackCheck, error) {
	read := assistantTestRead{Suite: assistantCreationSuite(cases)}
	return checkAssistantSavedPack(ctx, read, input.Candidate, assistantSavedPackCheckPorts{Fresh: input.Fresh, Call: call, Retain: func(ctx context.Context, _ *assistantPackCheck) error { return input.Fresh(ctx) }})
}

func assistantCreationCasesAndIssues(input *assistantCreationInput) ([]assistantCreationCase, []assistantCreationIssue, error) {
	casesRaw := input.State["cases"]
	if casesRaw == nil {
		casesRaw = []byte("[]")
	}
	cases, err := readAssistantCreationCases(casesRaw, input.Grounds)
	if err != nil {
		return nil, nil, err
	}
	issues := []assistantCreationIssue{}
	seen := map[string]bool{}
	for _, row := range cases {
		seen[row.ID] = true
	}
	if raw := input.State["expectationIssues"]; raw != nil {
		rows, err := assistantChatArray(raw)
		if err != nil || len(rows) > 256 {
			return nil, nil, errAssistantRunInvalid
		}
		for _, raw := range rows {
			fields, err := assistantTestObject(raw)
			if err != nil {
				return nil, nil, err
			}
			if fields["resolved"] != nil {
				continue // historical approval remains data in the original journal
			}
			original, err := readAssistantCreationCases(assistantChatEncodeArray([]json.RawMessage{fields["original"]}), input.Grounds)
			if err != nil {
				return nil, nil, err
			}
			if seen[original[0].ID] || len(seen) >= 256 {
				return nil, nil, errAssistantRunScope
			}
			seen[original[0].ID] = true
			message, err := assistantTestString(fields["message"])
			if err != nil {
				return nil, nil, err
			}
			finding := assistantCreationFinding{Status: "invalid", Message: message}
			if fields["finding"] != nil {
				f, err := assistantTestControls(fields["finding"], "status", "message", "admitted", "unreachable")
				if err != nil || assistantHostArguments(fields["finding"], &finding) != nil || finding.Status != "invalid" || finding.Message == "" || f["status"] == nil {
					return nil, nil, errAssistantRunInvalid
				}
			}
			issues = append(issues, assistantCreationIssue{ID: original[0].ID, Original: original[0], Finding: finding})
		}
	}
	return cases, issues, nil
}

// Invoked only inside a real admitted saved-chat execution. Model-free actions
// never open an adapter; reviewer actions use the caller's canonical adapter.
func (e *assistantRunExecution) executeAssistantCreation(ctx context.Context, review func(context.Context, *assistantCreationInput) (json.RawMessage, error)) (bool, error) {
	input, err := compileAssistantCreationInput(e.source)
	if err != nil || input == nil {
		return input != nil, err
	}
	if err = e.Authorize(ctx); err != nil {
		return true, err
	}
	if input.Action.Kind == "research-choice" {
		return false, nil // the canonical chat path owns this read-only request
	}
	var citations []assistantCreationCitation
	var proofs []assistantCreationSourceProof
	if input.Action.Kind != "recover" {
		citations, proofs, err = e.verifyAssistantCreationSources(ctx, input)
		if err != nil {
			return true, err
		}
	}
	runtime, err := e.assistantRuntime(ctx)
	if err != nil {
		return true, err
	}
	defer runtime.Close()
	operations := map[string]string{}
	call := func(ctx context.Context, name string, args any) (json.RawMessage, error) {
		if !contains([]string{"validate", "experimental_validate_expectations", "experimental_test_cases"}, name) {
			return nil, errAssistantRunScope
		}
		if err := input.Fresh(ctx); err != nil {
			return nil, err
		}
		capture := &assistantTestRuntimeReceipt{}
		raw, err := runtime.rpc(context.WithValue(ctx, assistantTestRuntimeReceiptKey{}, capture), "tools/call", map[string]any{"name": name, "arguments": args})
		if err == nil {
			operations[name] = capture.ID
		}
		return raw, err
	}
	cases, issues, err := assistantCreationCasesAndIssues(input)
	if err != nil {
		return true, err
	}
	unknowns := []string{}
	if raw := input.State["unknowns"]; raw != nil {
		if json.Unmarshal(raw, &unknowns) != nil || unknowns == nil {
			return true, errAssistantRunInvalid
		}
	}
	receipt := assistantCreationReceipt{Source: e.source.Original, Request: e.source.Request, Action: input.Action.Kind, Scope: input.Scope, Candidate: input.Candidate, Cases: cases, Issues: issues, Citations: citations, Sources: proofs, Unknowns: unknowns}
	switch input.Action.Kind {
	case "recover":
		turns, err := assistantChatArray(input.State["turns"])
		if err != nil {
			return true, err
		}
		found := false
		for index, raw := range turns {
			turn, err := assistantTestObject(raw)
			if err != nil {
				return true, err
			}
			if string(assistantChatTurnID(turn, index)) != string(assistantChatJSON(input.Action.MessageID)) {
				continue
			}
			text, err := assistantTestString(turn["text"])
			if err != nil || string(turn["role"]) != `"assistant"` || string(turn["kind"]) != `"message"` || digestOf([]byte(text)) != input.Action.MessageDigest {
				return true, errAssistantRunScope
			}
			receipt.Candidate, receipt.Unknowns, err = recoverAssistantCreationText(text)
			if err != nil {
				return true, err
			}
			input.Candidate = receipt.Candidate
			found = true
		}
		if !found {
			return true, errAssistantRunScope
		}
		receipt.Citations, receipt.Sources, err = e.verifyAssistantCreationSources(ctx, input)
		if err != nil {
			return true, err
		}
		receipt.Scope = input.Scope
	case "check":
		inputs := append([]assistantCreationCase{}, cases...)
		for _, issue := range issues {
			inputs = append(inputs, issue.Original)
		}
		findings, err := validateAssistantCreationExpectations(ctx, inputs, call)
		if err != nil {
			return true, err
		}
		for _, finding := range findings[:len(cases)] {
			if finding.Status != "valid" {
				return true, errAssistantRunScope
			}
		}
		receipt.ExpectationID = operations["experimental_validate_expectations"]
		if len(issues) > 0 {
			for i := range receipt.Issues {
				finding := findings[len(cases)+i]
				if finding.Status != "invalid" {
					return true, errAssistantRunScope
				}
				receipt.Issues[i].Finding = finding
			}
			if err := e.retainAssistantCreation(ctx, input, receipt); err != nil {
				return true, err
			}
			return true, e.completeAssistantCreation(ctx)
		}
	case "establish-cases", "retry-validation":
		if len(cases) != 0 || len(issues) != 0 {
			return true, errAssistantRunScope
		}
		var proposed []byte
		if input.Action.Kind == "retry-validation" {
			_, err := e.writer.lease.store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
				var err error
				receipt.HeldWitness, err = assistantCreationHeldWitness(*ledger, e.record, input, e.source.Original)
				return false, err
			})
			if err != nil {
				return true, err
			}
			held, err := assistantTestObject(input.State["heldProposal"])
			if err != nil || string(held["candidateDigest"]) != string(assistantChatJSON(input.Scope.CandidateDigest)) {
				return true, errAssistantRunScope
			}
			proposed = held["admitted"]
		} else {
			if raw := input.State["heldProposal"]; raw != nil && string(raw) != "null" {
				return true, errAssistantRunScope
			}
			validation, err := call(ctx, "validate", map[string]string{"document": input.Candidate})
			if err != nil {
				return true, err
			}
			payload, err := assistantSavedPackPayload(validation)
			if err != nil {
				return true, err
			}
			if string(payload["status"]) != `"valid"` {
				return true, failure.ToolInput("Check and repair the retained draft before establishing independent cases.")
			}
			if review == nil {
				return true, errAssistantRunUnavailable
			}
			result, err := review(ctx, input)
			if err != nil {
				return true, err
			}
			fields, err := assistantTestObject(result)
			if err != nil {
				return true, err
			}
			proposed = fields["cases"]
			observed, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
			if err != nil {
				return true, err
			}
			for _, event := range observed.Events {
				if event.Event.Proposal != nil {
					receipt.Unknowns = append(receipt.Unknowns, event.Event.Proposal.Unknowns...)
				}
			}
		}
		receipt.Held, err = readAssistantCreationCases(proposed, input.Grounds)
		if err != nil || len(receipt.Held) == 0 {
			return true, errAssistantRunScope
		}
		// Hold before validation: an interrupted validator is retried explicitly
		// against this exact complete proposal, without a new paid reviewer.
		if err = e.retainAssistantCreation(ctx, input, receipt); err != nil {
			return true, err
		}
		findings, err := validateAssistantCreationExpectations(ctx, receipt.Held, call)
		if err != nil {
			return true, err
		}
		receipt.ExpectationID = operations["experimental_validate_expectations"]
		for i, row := range receipt.Held {
			if findings[i].Status == "invalid" {
				receipt.Issues = append(receipt.Issues, assistantCreationIssue{ID: row.ID, Original: row, Finding: findings[i]})
			} else {
				row.ExpectedDisposition = []byte(findings[i].Canonical)
				receipt.Cases = append(receipt.Cases, row)
			}
		}
		receipt.Held = nil
		if err = e.retainAssistantCreation(ctx, input, receipt); err != nil {
			return true, err
		}
		if len(receipt.Issues) != 0 {
			return true, e.completeAssistantCreation(ctx)
		}
	case "propose-correction":
		if review == nil {
			return true, errAssistantRunUnavailable
		}
		issue := assistantCreationIssue{}
		for _, row := range issues {
			if row.ID == input.Action.IssueID {
				issue = row
			}
		}
		if issue.ID == "" {
			return true, errAssistantRunScope
		}
		currentFinding, err := validateAssistantCreationExpectations(ctx, []assistantCreationCase{issue.Original}, call)
		if err != nil {
			return true, err
		}
		if currentFinding[0].Status != "invalid" || !currentFinding[0].Admitted && !currentFinding[0].Unreachable || !assistantHostArgumentsMatch(assistantChatJSON(currentFinding[0]), assistantChatJSON(issue.Finding)) {
			return true, errAssistantRunScope
		}
		receipt.IssueValidationID = operations["experimental_validate_expectations"]
		result, err := review(ctx, input)
		if err != nil {
			return true, err
		}
		observed, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
		if err != nil {
			return true, err
		}
		for _, event := range observed.Events {
			if event.Event.Proposal != nil && len(event.Event.Proposal.Unknowns) > 0 {
				return true, errAssistantRunScope
			}
		}
		fields, err := assistantTestControls(result, "expectedDisposition", "rationale")
		if err != nil {
			return true, err
		}
		rationale, err := assistantTestString(fields["rationale"])
		if err != nil || strings.TrimSpace(rationale) == "" {
			return true, errAssistantRunInvalid
		}
		replacement := issue.Original
		replacement.ExpectedDisposition = fields["expectedDisposition"]
		findings, err := validateAssistantCreationExpectations(ctx, []assistantCreationCase{replacement}, call)
		if err != nil {
			return true, err
		}
		if findings[0].Status != "valid" {
			return true, errAssistantRunScope
		}
		proposal := assistantCreationCorrection{Scope: input.Scope, Original: issue.Original, ExpectedDisposition: []byte(findings[0].Canonical), Rationale: rationale}
		proposal.Token = digestOf(assistantChatJSON(proposal))
		receipt.Proposal = &proposal
		receipt.ExpectationID = operations["experimental_validate_expectations"]
		if err = e.retainAssistantCreation(ctx, input, receipt); err != nil {
			return true, err
		}
		return true, e.completeAssistantCreation(ctx)
	case "approve-correction":
		var current *assistantCreationCorrection
		_, err := e.writer.lease.store.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
			for _, run := range ledger.Runs {
				if run.Creation == nil || run.Creation.Proposal == nil || run.Creation.Proposal.Token != input.Action.Token || run.State != "complete" || run.Historical || run.ChatProjection == nil || !run.ChatProjection.Acknowledged {
					continue
				}
				proposal := *run.Creation.Proposal
				if proposal.Scope != input.Scope || run.Binding.Session != e.record.Binding.Session || run.Binding.Policy != e.record.Binding.Policy {
					return false, errAssistantRunScope
				}
				if current != nil {
					return false, errAssistantRunConflict
				}
				current = &proposal
				receipt.ProposalWitness = run.ID
			}
			return false, nil
		})
		if err != nil || current == nil {
			return true, errAssistantRunScope
		}
		issue := assistantCreationIssue{}
		for _, row := range issues {
			if row.ID == input.Action.IssueID {
				issue = row
			}
		}
		issue.Proposal = current
		receipt.Proposal = current
		if err := e.retainAssistantCreation(ctx, input, receipt); err != nil {
			return true, err
		}
		err = approveAssistantCreationCorrection(ctx, input.Scope, issue, input.Action.Token, cases, call, input.Fresh, func(ctx context.Context, staged []assistantCreationCase) (assistantPackCheck, error) {
			return e.checkAssistantCreation(ctx, input, staged, call)
		}, func(ctx context.Context, staged []assistantCreationCase, check assistantPackCheck) error {
			receipt.Cases, receipt.Check = staged, &check
			receipt.Approved = current
			receipt.Proposal = nil
			receipt.ValidationID, receipt.TestID, receipt.ExpectationID = operations["validate"], operations["experimental_test_cases"], operations["experimental_validate_expectations"]
			receipt.Issues = []assistantCreationIssue{}
			for _, row := range issues {
				if row.ID != issue.ID {
					receipt.Issues = append(receipt.Issues, row)
				}
			}
			return e.retainAssistantCreation(ctx, input, receipt)
		})
		if err != nil {
			return true, err
		}
		return true, e.completeAssistantCreation(ctx)
	default:
		return true, errAssistantRunInvalid
	}
	check, err := e.checkAssistantCreation(ctx, input, receipt.Cases, call)
	if err != nil {
		return true, err
	}
	receipt.Check = &check
	receipt.ValidationID, receipt.TestID = operations["validate"], operations["experimental_test_cases"]
	if err = e.retainAssistantCreation(ctx, input, receipt); err != nil {
		return true, err
	}
	return true, e.completeAssistantCreation(ctx)
}

func (e *assistantRunExecution) completeAssistantCreation(ctx context.Context) error {
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil || run.Checkpoint == nil {
		return errAssistantRunScope
	}
	cp := *run.Checkpoint
	cp.Status = "complete"
	if err = e.Checkpoint(ctx, cp); err != nil {
		return err
	}
	if err := e.writer.boundEvent(ctx, contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Creation review retained. Checks do not authorize Save."}}); err != nil {
		return err
	}
	if err := e.writer.State(ctx, "complete"); err != nil {
		return err
	}
	return e.ProjectChat(ctx)
}

func validateAssistantCreationReceipt(run assistantRunRecord) error {
	receipt := run.Creation
	if receipt == nil {
		return nil
	}
	if (receipt.Action == "retry-validation") != (receipt.HeldWitness != "") {
		return errAssistantRunScope
	}
	if (receipt.Action == "approve-correction") != (receipt.ProposalWitness != "") {
		return errAssistantRunScope
	}
	if run.Binding.Owner.Kind != "chat" || len(run.Binding.SourceDigests) != 1 || digestOf(receipt.Source) != run.Binding.SourceDigests[0] || digestOf(receipt.Request) != run.Binding.RequestDigest {
		return errAssistantRunScope
	}
	input, err := compileAssistantCreationInput(assistantRunSource{Original: receipt.Source, Request: receipt.Request, Binding: run.Binding})
	if err != nil || input == nil {
		return errAssistantRunScope
	}
	input.Scope.EvidenceDigest = receipt.Scope.EvidenceDigest
	if input.Action.Kind != receipt.Action || input.Scope != receipt.Scope || input.Action.Kind != "recover" && input.Candidate != receipt.Candidate {
		return errAssistantRunScope
	}
	input.Candidate = receipt.Candidate
	if !assistantCreationProofsMatch(input, receipt.Citations, receipt.Sources, receipt.Scope.EvidenceDigest) {
		return errAssistantRunScope
	}
	if receipt.Action == "recover" {
		turns, err := assistantChatArray(input.State["turns"])
		if err != nil {
			return err
		}
		matched := false
		for index, raw := range turns {
			turn, err := assistantTestObject(raw)
			if err != nil {
				return err
			}
			text, _ := assistantTestString(turn["text"])
			if string(assistantChatTurnID(turn, index)) == string(assistantChatJSON(input.Action.MessageID)) && string(turn["role"]) == `"assistant"` && string(turn["kind"]) == `"message"` && digestOf([]byte(text)) == input.Action.MessageDigest {
				document, unknowns, err := recoverAssistantCreationText(text)
				matched = err == nil && document == receipt.Candidate && string(assistantChatJSON(unknowns)) == string(assistantChatJSON(receipt.Unknowns))
			}
		}
		if !matched {
			return errAssistantRunScope
		}
	}
	if _, err := readAssistantCreationCases(assistantChatJSON(receipt.Cases), input.Grounds); err != nil {
		return err
	}
	if len(receipt.Held) > 0 {
		if _, err := readAssistantCreationCases(assistantChatJSON(receipt.Held), input.Grounds); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, row := range receipt.Cases {
		seen[row.ID] = true
	}
	for _, issue := range receipt.Issues {
		if seen[issue.ID] || issue.ID != issue.Original.ID || issue.Finding.Status != "invalid" || issue.Finding.Message == "" {
			return errAssistantRunInvalid
		}
		seen[issue.ID] = true
		if _, err := readAssistantCreationCases(assistantChatJSON([]assistantCreationCase{issue.Original}), input.Grounds); err != nil {
			return err
		}
	}
	if receipt.Check != nil && !validAssistantPackCheck(*receipt.Check, receipt.Candidate, assistantTestRead{Suite: assistantCreationSuite(receipt.Cases)}) {
		return errAssistantRunInvalid
	}
	if c := receipt.Check; c != nil {
		validation, err := assistantCreationRuntimePayload(run, "", receipt.ValidationID, "validate", map[string]string{"document": receipt.Candidate})
		if err != nil || (string(validation["status"]) == `"valid"`) != c.Valid || !assistantHostArgumentsMatch(validation["diagnostics"], assistantChatEncodeArray(c.Diagnostics)) {
			return errAssistantRunInvalid
		}
		if len(c.Report) > 0 {
			matrix, _, err := assistantSavedPackMatrix(assistantCreationSuite(receipt.Cases))
			if err != nil {
				return err
			}
			report, err := assistantCreationRuntimePayload(run, receipt.ValidationID, receipt.TestID, "experimental_test_cases", map[string]string{"pack": receipt.Candidate, "matrix": string(matrix)})
			if err != nil || !assistantHostArgumentsMatch(assistantChatEncodeObject(report), c.Report) {
				return errAssistantRunInvalid
			}
		} else if receipt.TestID != "" {
			return errAssistantRunInvalid
		}
	}
	for _, p := range []*assistantCreationCorrection{receipt.Proposal, receipt.Approved} {
		if p == nil {
			continue
		}
		unsigned := *p
		unsigned.Token = ""
		if p.Scope != receipt.Scope || p.Token != digestOf(assistantChatJSON(unsigned)) || strings.TrimSpace(p.Rationale) == "" {
			return errAssistantRunInvalid
		}
	}
	if err := validateAssistantCreationTransition(run, input); err != nil {
		return err
	}
	return nil
}

// Accept only a complete strict retained proposal. The original document bytes
// are extracted with the number-preserving data reader, never JSON roundtripped.
func recoverAssistantCreationText(text string) (string, []string, error) {
	var document string
	var unknowns []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if lines[i] != "```json" && lines[i] != "```" {
			continue
		}
		start := i + 1
		for i++; i < len(lines) && lines[i] != "```"; i++ {
		}
		if i == len(lines) {
			return "", nil, errAssistantRunInvalid
		}
		fields, err := assistantTestObject([]byte(strings.Join(lines[start:i], "\n")))
		if err != nil {
			return "", nil, errAssistantRunInvalid
		}
		if fields["proposal"] == nil {
			continue
		}
		if document != "" || len(fields) != 1 {
			return "", nil, errAssistantRunInvalid
		}
		proposal, err := assistantTestControls(fields["proposal"], "kind", "document", "unknowns")
		if err != nil || proposal["kind"] != nil && string(proposal["kind"]) != `"create"` || json.Unmarshal(proposal["unknowns"], &unknowns) != nil || unknowns == nil {
			return "", nil, errAssistantRunInvalid
		}
		pack, err := assistantTestObject(proposal["document"])
		if err != nil {
			return "", nil, err
		}
		for _, key := range []string{"specVersion", "id", "title"} {
			if _, err := assistantTestString(pack[key]); err != nil {
				return "", nil, err
			}
		}
		document = string(proposal["document"])
		if len(document) > 2_000_000 {
			return "", nil, errAssistantRunInvalid
		}
	}
	if document == "" {
		return "", nil, errAssistantRunInvalid
	}
	return document, unknowns, nil
}
