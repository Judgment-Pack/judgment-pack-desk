package desk

// Retry validates a complete proposal retained by the independent reviewer.
// A caller-supplied checkpoint, an old approval or assistant prose cannot mint
// independent expectations merely by naming a person's statement.
func assistantCreationHeldWitness(ledger assistantRunLedger, run assistantRunRecord, input *assistantCreationInput, source []byte) (string, error) {
	held, err := assistantTestObject(input.State["heldProposal"])
	if err != nil {
		return "", err
	}
	if string(held["candidateDigest"]) != string(assistantChatJSON(input.Scope.CandidateDigest)) {
		return "", errAssistantRunScope
	}
	var witness string
	for _, prior := range ledger.Runs {
		p := prior.Creation
		if p == nil || p.Action != "establish-cases" || len(p.Held) == 0 || prior.ID == run.ID || prior.Historical != run.Historical || !assistantRunTerminal(prior.State) || prior.ChatProjection == nil || !prior.ChatProjection.Acknowledged || prior.Binding.Session != run.Binding.Session || prior.Binding.Policy != run.Binding.Policy {
			continue
		}
		scope := p.Scope
		if scope.Principal != input.Scope.Principal || scope.Session != input.Scope.Session || scope.Project != input.Scope.Project || scope.Chat != input.Scope.Chat || scope.CandidateDigest != input.Scope.CandidateDigest || scope.EvidenceDigest != input.Scope.EvidenceDigest || p.Candidate != input.Candidate || !assistantHostArgumentsMatch(held["admitted"], assistantChatJSON(p.Held)) || !assistantHostArgumentsMatch(input.State["unknowns"], assistantChatJSON(p.Unknowns)) {
			continue
		}
		original, err := compileAssistantCreationInput(assistantRunSource{Original: p.Source, Request: p.Request, Binding: prior.Binding})
		if err != nil || original == nil || !assistantHostArgumentsMatch(assistantChatEncodeObject(input.Grounds), assistantChatEncodeObject(original.Grounds)) {
			continue
		}
		currentChat, err := assistantTestObject(source)
		if err != nil {
			return "", err
		}
		priorChat, err := assistantTestObject(p.Source)
		if err != nil {
			return "", err
		}
		currentDocs, priorDocs := currentChat["documents"], priorChat["documents"]
		if currentDocs == nil {
			currentDocs = []byte("[]")
		}
		if priorDocs == nil {
			priorDocs = []byte("[]")
		}
		if !assistantHostArgumentsMatch(currentDocs, priorDocs) {
			continue
		}
		if witness != "" {
			return "", errAssistantRunConflict
		}
		witness = prior.ID
	}
	if witness == "" {
		return "", errAssistantRunScope
	}
	return witness, nil
}

func validateAssistantCreationHeldWitnesses(ledger assistantRunLedger) error {
	for _, run := range ledger.Runs {
		if r := run.Creation; r != nil && r.Action == "approve-correction" {
			matched := false
			for _, prior := range ledger.Runs {
				if prior.ID != r.ProposalWitness || prior.ID == run.ID || prior.Creation == nil || prior.Creation.Action != "propose-correction" || prior.Creation.Proposal == nil || prior.State != "complete" || prior.Historical != run.Historical || prior.ChatProjection == nil || !prior.ChatProjection.Acknowledged || prior.Binding.Session != run.Binding.Session || prior.Binding.Policy != run.Binding.Policy {
					continue
				}
				proposal := r.Proposal
				if proposal == nil {
					proposal = r.Approved
				}
				matched = proposal != nil && assistantHostArgumentsMatch(assistantChatJSON(proposal), assistantChatJSON(prior.Creation.Proposal))
			}
			if !matched {
				return errAssistantRunScope
			}
		}
		if run.Creation == nil || run.Creation.Action != "retry-validation" {
			continue
		}
		r := run.Creation
		input, err := compileAssistantCreationInput(assistantRunSource{Original: r.Source, Request: r.Request, Binding: run.Binding})
		if err != nil || input == nil {
			return errAssistantRunScope
		}
		input.Scope.EvidenceDigest = r.Scope.EvidenceDigest
		witness, err := assistantCreationHeldWitness(ledger, run, input, r.Source)
		if err != nil || witness != r.HeldWitness {
			return errAssistantRunScope
		}
	}
	return nil
}
