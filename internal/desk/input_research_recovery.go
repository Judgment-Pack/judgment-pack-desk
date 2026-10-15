package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
)

// A source gap is returned only after the unsuccessful operation and retry
// disposition are durable. Storage/verification/transport errors never acquire
// this marker merely because the preceding attempt happened to be skippable.
type researchSourceGap struct {
	attemptID string
	cause     error
}

func (e *researchSourceGap) Error() string { return e.cause.Error() }
func (e *researchSourceGap) Unwrap() error { return e.cause }

func researchContinuableAttempt(a inputResearchAttempt) bool {
	if a.State != "uncertain" || (a.Kind != "page" && a.Kind != "search") {
		return false
	}
	// This code is emitted locally only for a successful acquire response that
	// exceeds the response cap, never for a seal, verification or disk failure.
	if a.ProblemCode == "source-response-too-large" || a.ProblemCode == "source-empty-text" {
		return true
	}
	if !a.AcquisitionRefusal {
		return false
	}
	if a.Kind == "search" {
		return a.ProblemCode == "search-not-grounded" || a.ProblemCode == "search-timeout" || a.ProblemCode == "search-temporary" || a.ProblemCode == "search-invalid-response"
	}
	switch a.ProblemCode {
	case "web-over-limit", "web-unsupported-type", "web-redirect-limit", "web-processing-failed":
		return true
	}
	// web-unavailable/provider-unavailable can include authentication or invalid
	// requests. Unknown and policy errors remain terminal until distinguishable.
	return false
}

func continueResearchSource(ctx context.Context, root *os.Root, failure error) (bool, error) {
	var gap *researchSourceGap
	if !errors.As(failure, &gap) {
		return false, failure
	}
	_, err := changeInputResearch(root, func(run *inputResearchRun) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if run.State != "running" || run.StopRequested || hasResearchRetry(*run, gap.attemptID) {
			return errors.New("Research is no longer available to continue.")
		}
		for i := range run.Attempts {
			a := &run.Attempts[i]
			if a.ID == gap.attemptID && researchContinuableAttempt(*a) {
				a.Continued = true
				return nil
			}
		}
		return errors.New("The source failure could not be recovered from saved history.")
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

type inputResearchSourceFailure struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Code  string `json:"code"`
	URL   string `json:"url,omitempty"`
	Query string `json:"query,omitempty"`
}

func researchSourceFailures(run inputResearchRun) []inputResearchSourceFailure {
	failures := []inputResearchSourceFailure{}
	for _, a := range run.Attempts {
		if a.Continued && researchContinuableAttempt(a) {
			failures = append(failures, inputResearchSourceFailure{ID: a.ID, Kind: a.Kind, Code: a.ProblemCode, URL: a.URL, Query: a.Query})
		}
	}
	return failures
}

// Explicit dependencies let deterministic tests exercise the real durable loop
// without provider traffic. Production still uses the reviewed Server bindings.
type inputResearchOperations struct {
	validateSearch func(context.Context, inputResearchRecipe) error
	validateAI     func(context.Context, inputResearchAI) error
	model          func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error)
	source         func(context.Context, *os.Root, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error)
	publish        func(inputResearchRun) error
}

func researchHasUnresolvedFields(run inputResearchRun) bool {
	for _, record := range run.Records {
		for _, value := range record.Fields {
			switch value.Status {
			case "missing", "conflicting", "inaccessible", "stale":
				return true
			}
		}
	}
	return false
}
