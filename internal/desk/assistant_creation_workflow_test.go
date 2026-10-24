package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const creationDisposition = `{"kind":"outcome","outcomeId":"accept","reasons":[],"handoff":{"state":"none"}}`

func creationFixture(t *testing.T) (assistantCreationScope, assistantCreationIssue, []assistantCreationCase) {
	t.Helper()
	rows, err := readAssistantCreationCases([]byte(`[{"id":"typed","facts":{"zero":0,"false":false,"null":null,"numeric":"1","large":9007199254740993},"evidenceAvailability":false,"expectedDisposition":`+creationDisposition+`,"expectedHandoffTarget":null,"expectationSource":"message:user","rationale":"The person supplied this decision."}]`), map[string]json.RawMessage{"message:user": []byte(`"Independent user statement"`)})
	if err != nil {
		t.Fatal(err)
	}
	scope := assistantCreationScope{"principal", "session", "project", "chat", 1, strings.Repeat("a", 64), strings.Repeat("b", 64), ""}
	issue := assistantCreationIssue{ID: "typed", Original: rows[0], Finding: assistantCreationFinding{Status: "invalid", Message: "Invalid reason", Admitted: true}}
	issue.Proposal = &assistantCreationCorrection{Token: "visible-token", Scope: scope, Original: rows[0], ExpectedDisposition: []byte(creationDisposition), Rationale: "The independent statement establishes yes."}
	return scope, issue, rows
}

func creationValidation(_ context.Context, name string, args any) (json.RawMessage, error) {
	if name != "experimental_validate_expectations" {
		return nil, errAssistantRunInvalid
	}
	return assistantChatJSON(map[string]any{"structuredContent": map[string]any{"specVersion": "0.2.0-draft", "status": "valid", "results": []any{map[string]any{"index": 0, "status": "valid", "canonical": creationDisposition}}}}), nil
}

func creationCheck(scope assistantCreationScope, ids ...string) assistantPackCheck {
	rows := []any{}
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "status": "passed", "expected": creationDisposition, "actual": creationDisposition})
	}
	return assistantPackCheck{DocumentDigest: scope.CandidateDigest, TotalCases: len(ids), Valid: true, Report: assistantChatJSON(map[string]any{"status": "passed", "summary": map[string]int{"total": len(ids), "passed": len(ids)}, "packs": []any{map[string]any{"rows": rows}}})}
}

func TestAssistantCreationCasesPreserveExactInputs(t *testing.T) {
	_, issue, _ := creationFixture(t)
	row := issue.Original
	if string(row.Facts) != `{"zero":0,"false":false,"null":null,"numeric":"1","large":9007199254740993}` || string(row.EvidenceAvailability) != "false" || string(row.ExpectedHandoffTarget) != "null" {
		t.Fatal("typed facts or optional assertions changed", row)
	}
	raw := assistantChatJSON([]assistantCreationCase{row, row})
	if _, err := readAssistantCreationCases(raw, map[string]json.RawMessage{row.ExpectationSource: []byte(`"statement"`)}); err == nil {
		t.Fatal("duplicate case ID admitted")
	}
	if _, err := readAssistantCreationCases(assistantChatJSON([]assistantCreationCase{row}), map[string]json.RawMessage{"candidate:rules": []byte(`"observed yes"`)}); err == nil {
		t.Fatal("ungrounded expectation admitted")
	}
}

func TestAssistantCreationCorrectionExactBinding(t *testing.T) {
	scope, issue, _ := creationFixture(t)
	for _, mutation := range []struct {
		name string
		edit func(*assistantCreationScope, *assistantCreationIssue, *string, *[]assistantCreationCase)
	}{
		{"session", func(s *assistantCreationScope, _ *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			s.Session = "replacement"
		}},
		{"project", func(s *assistantCreationScope, _ *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			s.Project = "replacement"
		}},
		{"chat", func(s *assistantCreationScope, _ *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			s.Chat = "replacement"
		}},
		{"generation", func(s *assistantCreationScope, _ *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			s.Generation++
		}},
		{"source", func(s *assistantCreationScope, _ *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			s.SourceDigest = strings.Repeat("c", 64)
		}},
		{"candidate", func(s *assistantCreationScope, _ *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			s.CandidateDigest = strings.Repeat("d", 64)
		}},
		{"token", func(_ *assistantCreationScope, _ *assistantCreationIssue, token *string, _ *[]assistantCreationCase) {
			*token = "historical-token"
		}},
		{"original", func(_ *assistantCreationScope, i *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			i.Original.ExpectedDisposition = []byte(`{}`)
		}},
		{"target", func(_ *assistantCreationScope, i *assistantCreationIssue, _ *string, _ *[]assistantCreationCase) {
			i.Original.ExpectedHandoffTarget = []byte(`"different"`)
		}},
		{"duplicate", func(_ *assistantCreationScope, i *assistantCreationIssue, _ *string, rows *[]assistantCreationCase) {
			*rows = append(*rows, i.Original)
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			s, i, token, rows := scope, issue, issue.Proposal.Token, []assistantCreationCase{}
			mutation.edit(&s, &i, &token, &rows)
			if _, err := assistantCreationCorrectedCase(s, i, *i.Proposal, token, rows); err == nil {
				t.Fatal("stale correction accepted")
			}
		})
	}
}

func TestAssistantCreationCorrectionRetestBeforeCommit(t *testing.T) {
	for _, mode := range []string{"complete", "interrupted", "failed", "short-suite", "changed-candidate", "cas"} {
		t.Run(mode, func(t *testing.T) {
			scope, issue, _ := creationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			commits, tested := 0, false
			err := approveAssistantCreationCorrection(ctx, scope, issue, issue.Proposal.Token, nil, creationValidation, func(context.Context) error { return nil }, func(ctx context.Context, rows []assistantCreationCase) (assistantPackCheck, error) {
				tested = true
				if len(rows) != 1 || string(rows[0].Facts) != string(issue.Original.Facts) || string(rows[0].ExpectedHandoffTarget) != "null" {
					t.Fatal("retest changed original facts or target")
				}
				if mode == "interrupted" {
					cancel()
				}
				if mode == "failed" {
					return assistantPackCheck{}, errors.New("transport uncertainty")
				}
				check := creationCheck(scope, issue.ID)
				if mode == "short-suite" {
					check.TotalCases = 0
				}
				if mode == "changed-candidate" {
					check.DocumentDigest = strings.Repeat("f", 64)
				}
				return check, nil
			}, func(context.Context, []assistantCreationCase, assistantPackCheck) error {
				if mode == "cas" {
					return errAssistantRunConflict
				}
				commits++
				return nil
			})
			if !tested || (mode == "complete") != (err == nil) || commits != map[bool]int{true: 1, false: 0}[mode == "complete"] {
				t.Fatal("approval committed without completed exact retest/CAS", mode, tested, commits, err)
			}
		})
	}
}

func TestAssistantCreationExpectationReportIsComplete(t *testing.T) {
	_, issue, _ := creationFixture(t)
	for _, results := range []string{`[]`, `[{"status":"valid","canonical":{}}]`, `[{"index":1,"status":"valid","canonical":"{}"}]`, `[{"index":0,"status":"invalid","message":""}]`} {
		_, err := validateAssistantCreationExpectations(context.Background(), []assistantCreationCase{issue.Original}, func(context.Context, string, any) (json.RawMessage, error) {
			return []byte(`{"structuredContent":{"specVersion":"0.2.0-draft","results":` + results + `}}`), nil
		})
		if err == nil {
			t.Fatal("incomplete expectation report admitted", results)
		}
	}
}
