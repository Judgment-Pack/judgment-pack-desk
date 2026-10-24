package desk

import "encoding/json"

func projectAssistantCreation(state map[string]json.RawMessage, run assistantRunRecord, responseID string) error {
	if err := validateAssistantCreationReceipt(run); err != nil {
		return err
	}
	r := run.Creation
	if r.Action == "approve-correction" && run.State != "complete" {
		return nil
	}
	state["cases"] = assistantChatJSON(r.Cases)
	state["unknowns"] = assistantChatJSON(r.Unknowns)
	issues := []json.RawMessage{}
	previous, err := assistantChatArray(state["expectationIssues"])
	if err != nil {
		return err
	}
	for _, raw := range previous {
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		if fields["resolved"] != nil {
			issues = append(issues, raw)
		}
	}
	for _, issue := range r.Issues {
		issues = append(issues, assistantChatJSON(map[string]any{"id": issue.ID, "original": issue.Original, "message": issue.Finding.Message, "finding": issue.Finding}))
	}
	if approval := r.Approved; approval != nil {
		for _, replacement := range r.Cases {
			if replacement.ID == approval.Original.ID {
				issues = append(issues, assistantChatJSON(map[string]any{"id": replacement.ID, "original": approval.Original, "message": "The expectation correction was explicitly approved and retested.", "resolved": map[string]any{"replacement": replacement, "approvedAt": r.Check.At, "rationale": approval.Rationale}}))
			}
		}
	}
	state["expectationIssues"] = assistantChatEncodeArray(issues)
	state["heldProposal"] = []byte("null")
	if len(r.Held) > 0 {
		state["heldProposal"] = assistantChatJSON(map[string]any{"candidateDigest": r.Scope.CandidateDigest, "admitted": r.Held, "dropped": []any{}, "unknowns": r.Unknowns})
	}
	if r.Action == "recover" {
		state["candidates"] = assistantChatJSON([]any{map[string]any{"text": r.Candidate, "responseId": responseID}})
		state["unknowns"] = assistantChatJSON(r.Unknowns)
	}
	if r.Check != nil {
		candidates, err := assistantChatArray(state["candidates"])
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return errAssistantRunScope
		}
		candidate, err := assistantTestObject(candidates[len(candidates)-1])
		if err != nil {
			return err
		}
		rows := []json.RawMessage{}
		if len(r.Check.Report) > 0 {
			report, _ := assistantTestObject(r.Check.Report)
			packs, _ := assistantChatArray(report["packs"])
			pack, _ := assistantTestObject(packs[0])
			results, _ := assistantChatArray(pack["rows"])
			for _, raw := range results {
				row, _ := assistantTestObject(raw)
				rows = append(rows, assistantChatJSON(map[string]any{"id": json.RawMessage(row["id"]), "passed": string(row["status"]) == `"passed"`, "expected": json.RawMessage(row["expected"]), "actual": json.RawMessage(row["actual"])}))
			}
		}
		candidate["previousCheck"] = assistantChatJSON(map[string]any{"documentDigest": r.Check.DocumentDigest, "valid": r.Check.Valid, "diagnostics": r.Check.Diagnostics, "cases": rows})
		candidates[len(candidates)-1] = assistantChatEncodeObject(candidate)
		state["candidates"] = assistantChatEncodeArray(candidates)
	}
	state["readiness"] = assistantChatJSON("")
	if len(r.Issues) > 0 || len(r.Held) > 0 {
		state["status"] = assistantChatJSON("needs-input")
	}
	return nil
}

type assistantCreationView struct {
	Action          string                           `json:"action"`
	CandidateDigest string                           `json:"candidateDigest"`
	SourceDigest    string                           `json:"sourceDigest"`
	Generation      int                              `json:"generation"`
	Candidate       string                           `json:"candidate"`
	Cases           string                           `json:"cases"`
	Held            string                           `json:"held"`
	Issues          string                           `json:"issues"`
	Check           *assistantPackCheck              `json:"check,omitempty"`
	Proposal        *assistantCreationCorrectionView `json:"proposal,omitempty"`
	Citations       []assistantCreationCitation      `json:"citations"`
	Unknowns        []string                         `json:"unknowns"`
}

type assistantCreationCorrectionView struct {
	Token               string          `json:"token"`
	IssueID             string          `json:"issueId"`
	ExpectedDisposition json.RawMessage `json:"expectedDisposition"`
	Rationale           string          `json:"rationale"`
}

func assistantCreationPublic(run assistantRunRecord) *assistantCreationView {
	r := run.Creation
	if r == nil {
		return nil
	}
	if r.Action == "approve-correction" && run.State != "complete" && r.Approved != nil {
		copy := *r
		input, err := compileAssistantCreationInput(assistantRunSource{Original: r.Source, Request: r.Request, Binding: run.Binding})
		if err != nil || input == nil {
			return nil
		}
		copy.Cases, copy.Issues, err = assistantCreationCasesAndIssues(input)
		if err != nil {
			return nil
		}
		copy.Proposal, copy.Approved, copy.Check = r.Approved, nil, nil
		r = &copy
	}
	cases, held, issues := r.Cases, r.Held, r.Issues
	if cases == nil {
		cases = []assistantCreationCase{}
	}
	if held == nil {
		held = []assistantCreationCase{}
	}
	if issues == nil {
		issues = []assistantCreationIssue{}
	}
	view := &assistantCreationView{Action: r.Action, CandidateDigest: digestOf([]byte(r.Candidate)), SourceDigest: r.Scope.SourceDigest, Generation: r.Scope.Generation, Candidate: r.Candidate, Cases: string(assistantChatJSON(cases)), Held: string(assistantChatJSON(held)), Issues: string(assistantChatJSON(issues)), Check: r.Check}
	view.Citations = r.Citations
	view.Unknowns = r.Unknowns
	if p := r.Proposal; p != nil && !run.Historical && (run.State == "complete" || r.Action == "approve-correction") {
		view.Proposal = &assistantCreationCorrectionView{Token: p.Token, IssueID: p.Original.ID, ExpectedDisposition: p.ExpectedDisposition, Rationale: p.Rationale}
	}
	return view
}
