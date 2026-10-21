package desk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func assistantPolicyObservationShape(t *testing.T, f *policySourceFixture, shape, at string) json.RawMessage {
	t.Helper()
	raw := policyObservationFixture(t, f.s, f.binding, "", at, 0)
	var observation policyGatewayObservation
	assistantRunMust(t, json.Unmarshal(raw, &observation))
	var document map[string]any
	assistantRunMust(t, json.Unmarshal(observation.Document, &document))
	content := document["content"].(map[string]any)
	processing := document["processing"].(map[string]any)
	switch shape {
	case "partial":
		content["truncated"] = true
		processing["status"] = "partial"
	case "unavailable":
		content["pages"] = []any{}
		content["pageCount"] = 0
		content["chars"] = 0
		content["extraction"] = "none"
		processing["status"] = "failed"
		processing["errors"] = []any{map[string]any{"code": "timeout", "message": "Fixture extraction timeout", "page": nil}}
	}
	observation.Document, _ = json.Marshal(document)
	canonical, err := f.s.canonicalPolicySource(t.Context(), observation.Document)
	assistantRunMust(t, err)
	observation.Document = canonical
	observation.Extraction.Content.BytesDigest = "sha256:" + digestOf(canonical)
	observation.Extraction.Content.ByteLength = len(canonical)
	observation.ObservationID = digestOf([]byte(f.binding.BindingID + shape + at))
	raw, _ = json.Marshal(observation)
	return raw
}
func assistantPolicyCompare(t *testing.T, service policyService, id string, before, after assistantPolicySelector) map[string]any {
	t.Helper()
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	v, err := service.assistantPolicyComparison(t.Context(), id, a, b)
	assistantRunMust(t, err)
	return v.(map[string]any)
}
func TestAssistantPolicyComparisonRetainedSourcesAndLimitations(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	first := f.sync(t).State.Observations[0]
	f.action(t, "revoke", nil)
	f.action(t, "bind", func(q *policySourceSyncRequest) {
		q.ResourceID = "file-B"
		q.SelectionContext = strings.Repeat("b", 64)
		q.Consent = "recurring-read-after-sign-out"
		q.FrequencyMinutes = 15
	})
	f.poll(t)
	observations := f.sync(t).State.Observations
	if len(observations) != 2 {
		t.Fatal("historical binding observation lost", len(observations))
	}
	second := observations[1]
	f.observation = assistantPolicyObservationShape(t, f, "partial", "2026-10-18T06:40:17Z")
	f.poll(t)
	observations = f.sync(t).State.Observations
	partial := observations[len(observations)-1]
	f.observation = assistantPolicyObservationShape(t, f, "unavailable", "2026-10-18T06:40:18Z")
	f.poll(t)
	observations = f.sync(t).State.Observations
	unavailable := observations[len(observations)-1]
	service, _ := coverageServiceFixture(f.s, f.token)
	beforeCalls, beforeArtifacts := len(f.calls), f.artifactCount(t)
	selector := func(id string) assistantPolicySelector {
		return assistantPolicySelector{Kind: "observation", ObservationID: id}
	}
	equal := assistantPolicyCompare(t, service, f.policy.Document.ID, selector(first.ID), selector(second.ID))
	limits := equal["limitations"].([]string)
	for _, want := range []string{"informational-only", "snapshot-only", "different-source-binding", "equal-extracted-text"} {
		if !contains(limits, want) {
			t.Fatal("comparison dropped limitation", want, limits)
		}
	}
	a, b := equal["before"].(assistantPolicyComparisonSide), equal["after"].(assistantPolicyComparisonSide)
	if a.Text == nil || b.Text == nil || *a.Text != *b.Text || a.Identity["bindingId"] == b.Identity["bindingId"] {
		t.Fatal("equal extraction erased source identity")
	}
	mixed := assistantPolicyCompare(t, service, f.policy.Document.ID, assistantPolicySelector{Kind: "revision", RevisionID: f.policy.Document.Candidate.RevisionID}, selector(partial.ID))
	limits = mixed["limitations"].([]string)
	for _, want := range []string{"informational-only", "snapshot-only", "mixed-projections", "incomplete-extraction"} {
		if !contains(limits, want) {
			t.Fatal("partial comparison dropped limitation", want)
		}
	}
	if mixed["after"].(assistantPolicyComparisonSide).Metadata["completeness"] != "partial" {
		t.Fatal("partial extraction called complete")
	}
	missing := assistantPolicyCompare(t, service, f.policy.Document.ID, selector(partial.ID), selector(unavailable.ID))
	if !contains(missing["limitations"].([]string), "unavailable-extraction") || missing["after"].(assistantPolicyComparisonSide).Text != nil {
		t.Fatal("unavailable extraction invented text")
	}
	if len(f.calls) != beforeCalls || f.artifactCount(t) != beforeArtifacts {
		t.Fatal("comparison acquired or saved source data")
	}
	// Reusing the current service boundary must not permit a historical read
	// after its authorization ends, nor resolve a nonexistent selector as latest.
	raw, _ := json.Marshal(selector(first.ID))
	foreign, _ := json.Marshal(selector(strings.Repeat("f", 64)))
	if _, err := service.assistantPolicyComparison(t.Context(), f.policy.Document.ID, raw, foreign); err == nil {
		t.Fatal("unknown selector fell back to a current source")
	}
	broken := service
	broken.artifacts = assistantPolicyCorruptArtifact{retainedArtifactRepository: service.artifacts, target: first.Original.SHA256}
	if _, err := broken.assistantPolicyComparison(t.Context(), f.policy.Document.ID, raw, raw); err == nil {
		t.Fatal("comparison accepted corrupt original bytes")
	}
	f.s.sessions.revoke(f.token)
	if _, err := service.assistantPolicyComparison(t.Context(), f.policy.Document.ID, raw, raw); err == nil {
		t.Fatal("historical comparison outlived current access")
	}
}

type assistantPolicyCorruptArtifact struct {
	retainedArtifactRepository
	target string
}

func (r assistantPolicyCorruptArtifact) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	raw, err := r.retainedArtifactRepository.Get(ctx, ref)
	if err == nil && ref.SHA256 == r.target {
		raw = append([]byte(nil), raw...)
		raw[0] ^= 1
	}
	return raw, err
}
func TestAssistantPolicyWithdrawnPublicationIsHistoricalSource(t *testing.T) {
	s, token, e, p, _ := assistantPolicyFixture(t, "http://127.0.0.1:1", "work", false, func(s *Server, token string, p policyReply) policyReply {
		p = policyApprove(t, s, token, p)
		return policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/publish", policyPublishBody(t, p))).Policy
	})
	x := assistantPolicyOpen(t, s, e)
	service, _ := coverageServiceFixture(s, token)
	q := lifecycleBody(t, service, p, "withdraw")
	q.Rationale = "Retain withdrawn release as historical reference."
	policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+p.Document.ID+"/lifecycle", q))
	result := assistantPolicyInvoke(t, e, x, "historical-handoff", "propose_policy_graph_interpretation", map[string]string{"publicationId": p.Document.Publications[0].ID})
	if result.Err() {
		t.Fatal("withdrawal denied historical source access", result.Value().Text())
	}
	out := assistantExecutionRecord(t, e).DomainOutputs[0]
	if out.PolicySource.PublicationID != p.Document.Publications[0].ID || out.PolicySource.RevisionID != p.Document.Publications[0].CandidateRevisionID || out.PolicySource.Narrative != p.Document.Candidate.Narrative {
		t.Fatal("withdrawn publication was replaced by latest draft")
	}
	view, err := service.lifecycle(t.Context(), p.Document.ID)
	assistantRunMust(t, err)
	if view.Releases[0].Status != "withdrawn" {
		t.Fatal("historical read restored governing status")
	}
}
