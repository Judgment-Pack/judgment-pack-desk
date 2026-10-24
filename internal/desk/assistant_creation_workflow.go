package desk

// Creation checks consume independently supplied cases. Runtime observations
// never become expected answers, and a staged correction is not an approval.
import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

type assistantCreationCase struct {
	ID                    string          `json:"id"`
	Facts                 json.RawMessage `json:"facts"`
	EvidenceAvailability  json.RawMessage `json:"evidenceAvailability,omitempty"`
	ExpectedDisposition   json.RawMessage `json:"expectedDisposition"`
	ExpectedHandoffTarget json.RawMessage `json:"expectedHandoffTarget,omitempty"`
	ExpectationSource     string          `json:"expectationSource"`
	Rationale             string          `json:"rationale"`
}

type assistantCreationFinding struct {
	Status      string `json:"status"`
	Canonical   string `json:"canonical,omitempty"`
	Message     string `json:"message,omitempty"`
	Admitted    bool   `json:"admitted,omitempty"`
	Unreachable bool   `json:"unreachable,omitempty"`
}

type assistantCreationIssue struct {
	ID       string                       `json:"id"`
	Original assistantCreationCase        `json:"original"`
	Finding  assistantCreationFinding     `json:"finding"`
	Proposal *assistantCreationCorrection `json:"proposal,omitempty"`
}

// Scope is host-captured on the explicit action, never decoded from model text.
// The source digest includes original statements, citations and typed cases.
type assistantCreationScope struct {
	Principal       string `json:"principal"`
	Session         string `json:"session"`
	Project         string `json:"project"`
	Chat            string `json:"chat"`
	Generation      int    `json:"generation"`
	SourceDigest    string `json:"sourceDigest"`
	CandidateDigest string `json:"candidateDigest"`
	EvidenceDigest  string `json:"evidenceDigest"`
}

type assistantCreationCorrection struct {
	Token               string                 `json:"token"`
	Scope               assistantCreationScope `json:"scope"`
	Original            assistantCreationCase  `json:"original"`
	ExpectedDisposition json.RawMessage        `json:"expectedDisposition"`
	Rationale           string                 `json:"rationale"`
}

type assistantCreationCall func(context.Context, string, any) (json.RawMessage, error)

func readAssistantCreationCases(raw []byte, grounds map[string]json.RawMessage) ([]assistantCreationCase, error) {
	rows, err := assistantChatArray(raw)
	if err != nil || len(rows) > 256 {
		return nil, errAssistantRunInvalid
	}
	cases := make([]assistantCreationCase, 0, len(rows))
	seen := map[string]bool{}
	for _, raw := range rows {
		fields, err := assistantTestControls(raw, "id", "facts", "evidenceAvailability", "expectedDisposition", "expectedHandoffTarget", "expectationSource", "rationale")
		var row assistantCreationCase
		if err != nil || assistantHostArguments(raw, &row) != nil || !assistantRunText(row.ID, 128) || seen[row.ID] || strings.TrimSpace(row.Rationale) == "" || len(row.Rationale) > 8000 || grounds[row.ExpectationSource] == nil {
			return nil, errAssistantRunScope
		}
		if _, err := assistantTestObject(fields["facts"]); err != nil || fields["expectedDisposition"] == nil {
			return nil, errAssistantRunInvalid
		}
		// Retain missing, false, zero, null, string and large-number spellings.
		// Runtime owns their semantic interpretation; Desk never coerces them.
		seen[row.ID] = true
		cases = append(cases, row)
	}
	return cases, nil
}

func validateAssistantCreationExpectations(ctx context.Context, cases []assistantCreationCase, call assistantCreationCall) ([]assistantCreationFinding, error) {
	if len(cases) > 256 {
		return nil, errAssistantRunInvalid
	}
	if len(cases) == 0 {
		return []assistantCreationFinding{}, nil
	}
	expectations := make([]string, len(cases))
	for i, row := range cases {
		expectations[i] = string(row.ExpectedDisposition)
	}
	raw, err := call(ctx, "experimental_validate_expectations", map[string]any{"spec_version": "0.2.0-draft", "expectations": expectations})
	if err != nil {
		return nil, err
	}
	report, err := assistantSavedPackPayload(raw)
	if err != nil || string(report["specVersion"]) != `"0.2.0-draft"` {
		return nil, errAssistantRunInvalid
	}
	rows, err := assistantChatArray(report["results"])
	if err != nil || len(rows) != len(cases) {
		return nil, errAssistantRunInvalid
	}
	findings := make([]assistantCreationFinding, len(rows))
	invalid := false
	for i, raw := range rows {
		fields, err := assistantTestObject(raw)
		var row struct {
			Index     int    `json:"index"`
			Status    string `json:"status"`
			Canonical string `json:"canonical"`
			Message   string `json:"message"`
			Code      string `json:"code"`
		}
		if err != nil || fields["index"] == nil || json.Unmarshal(raw, &row) != nil || row.Index != i {
			return nil, errAssistantRunInvalid
		}
		finding := assistantCreationFinding{Status: row.Status}
		switch row.Status {
		case "valid":
			if _, err := assistantTestObject([]byte(row.Canonical)); err != nil {
				return nil, err
			}
			finding.Canonical = row.Canonical
			if assistantCreationTargetContradiction([]byte(row.Canonical), cases[i].ExpectedHandoffTarget) {
				finding = assistantCreationFinding{Status: "invalid", Admitted: true, Message: "The expected handoff target contradicts the expected disposition."}
			}
		case "invalid":
			invalid = true
			if strings.TrimSpace(row.Message) == "" {
				return nil, errAssistantRunInvalid
			}
			finding.Message = row.Message
			finding.Unreachable = row.Code == "JPS-EXPECTATION-UNREACHABLE"
			finding.Admitted = row.Code != "JPS-EXPECTATION-LIMIT"
		default:
			return nil, errAssistantRunInvalid
		}
		findings[i] = finding
	}
	status := `"valid"`
	if invalid {
		status = `"invalid"`
	}
	if string(report["status"]) != status {
		return nil, errAssistantRunInvalid
	}
	return findings, nil
}

func assistantCreationTargetContradiction(disposition, target []byte) bool {
	if len(target) == 0 || bytes.Equal(bytes.TrimSpace(target), []byte("null")) {
		return false
	}
	fields, err := assistantTestObject(disposition)
	if err != nil {
		return true
	}
	// Match the creation contract: an explicit non-null target requires a
	// requested handoff. A requested handoff with no target is pack-dependent.
	handoff, err := assistantTestObject(fields["handoff"])
	return err != nil || string(handoff["state"]) != `"requested"`
}

func assistantCreationCorrectedCase(scope assistantCreationScope, issue assistantCreationIssue, proposal assistantCreationCorrection, token string, cases []assistantCreationCase) (assistantCreationCase, error) {
	if proposal.Token == "" || token != proposal.Token || scope != proposal.Scope || issue.ID != issue.Original.ID || issue.Finding.Status != "invalid" || !assistantHostArgumentsMatch(assistantChatJSON(issue.Original), assistantChatJSON(proposal.Original)) {
		return assistantCreationCase{}, errAssistantRunScope
	}
	for _, row := range cases {
		if row.ID == issue.ID {
			return assistantCreationCase{}, errAssistantRunConflict
		}
	}
	if assistantCreationTargetContradiction(proposal.ExpectedDisposition, issue.Original.ExpectedHandoffTarget) {
		return assistantCreationCase{}, errAssistantRunScope
	}
	replacement := issue.Original
	replacement.ExpectedDisposition = bytes.Clone(proposal.ExpectedDisposition)
	return replacement, nil
}

// Retest first, then CAS the result. Failure/Stop/uncertain delivery leaves the
// original issue unresolved; no temporary replacement ever becomes current.
func approveAssistantCreationCorrection(ctx context.Context, scope assistantCreationScope, issue assistantCreationIssue, token string, cases []assistantCreationCase, call assistantCreationCall, fresh func(context.Context) error, retest func(context.Context, []assistantCreationCase) (assistantPackCheck, error), commit func(context.Context, []assistantCreationCase, assistantPackCheck) error) error {
	if issue.Proposal == nil || fresh(ctx) != nil {
		return errAssistantRunScope
	}
	replacement, err := assistantCreationCorrectedCase(scope, issue, *issue.Proposal, token, cases)
	if err != nil {
		return err
	}
	findings, err := validateAssistantCreationExpectations(ctx, []assistantCreationCase{replacement}, call)
	if err != nil {
		return err
	}
	if findings[0].Status != "valid" || !assistantHostArgumentsMatch([]byte(findings[0].Canonical), replacement.ExpectedDisposition) {
		return errAssistantRunScope
	}
	staged := append(append([]assistantCreationCase{}, cases...), replacement)
	if err = fresh(ctx); err != nil {
		return err
	}
	check, err := retest(ctx, staged)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if check.DocumentDigest != scope.CandidateDigest || check.TotalCases != len(staged) || !check.Valid || len(check.Report) == 0 {
		return errAssistantRunScope
	}
	ids := make([]string, len(staged))
	for i := range staged {
		ids[i] = staged[i].ID
	}
	if !validAssistantSavedPackReport(check.Report, ids) {
		return errAssistantRunScope
	}
	if err = fresh(ctx); err != nil {
		return err
	}
	return commit(ctx, staged, check)
}
