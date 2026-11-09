package desk

import (
	"context"
	"encoding/json"
	"strings"
)

type assistantPolicySelector struct {
	Kind          string `json:"kind"`
	RevisionID    string `json:"revisionId,omitempty"`
	PublicationID string `json:"publicationId,omitempty"`
	ObservationID string `json:"observationId,omitempty"`
}

func readAssistantPolicySelector(raw json.RawMessage) (assistantPolicySelector, error) {
	var s assistantPolicySelector
	members, err := configMembers(raw)
	if err != nil || len(members) != 2 || assistantHostArguments(raw, &s) != nil {
		return s, errAssistantRunInvalid
	}
	expected := ""
	switch s.Kind {
	case "revision":
		if policyEventIDForm.MatchString(s.RevisionID) && strings.HasPrefix(s.RevisionID, "revision_") {
			expected = "revisionId"
		}
	case "publication":
		if policyEventIDForm.MatchString(s.PublicationID) && strings.HasPrefix(s.PublicationID, "publication_") {
			expected = "publicationId"
		}
	case "observation":
		if assistantRunDigest(s.ObservationID) {
			expected = "observationId"
		}
	}
	if expected == "" {
		return s, errAssistantRunInvalid
	}
	for _, m := range members {
		if m.name != "kind" && m.name != expected {
			return s, errAssistantRunInvalid
		}
	}
	return s, nil
}

type assistantPolicyComparisonSide struct {
	Selector assistantPolicySelector `json:"selector"`
	Identity map[string]any          `json:"identity"`
	Text     *string                 `json:"text"`
	Metadata map[string]any          `json:"metadata"`
}

func (s policyService) assistantPolicySavedSide(ctx context.Context, id string, selector assistantPolicySelector) (assistantPolicyComparisonSide, error) {
	side := assistantPolicyComparisonSide{Selector: selector}
	source, err := s.authoringSource(ctx, id, selector.RevisionID, selector.PublicationID)
	if err != nil {
		return side, err
	}
	revision, err := s.revision(ctx, id, source.RevisionID)
	if err != nil {
		return side, err
	}
	var retained struct {
		Candidate      policyCandidate `json:"candidate"`
		Workflow       policyWorkflow  `json:"workflow"`
		WorkflowDigest string          `json:"workflowDigest"`
	}
	raw, _ := json.Marshal(revision)
	if json.Unmarshal(raw, &retained) != nil {
		return side, errAssistantRunInvalid
	}
	c := retained.Candidate
	if c.RevisionID != source.RevisionID || c.Digest != source.CandidateDigest || c.Content != source.Content || c.Title != source.Title || c.ArtifactVersion != source.ArtifactVersion || c.Narrative != source.Narrative || !assistantRunDigest(retained.WorkflowDigest) {
		return side, errAssistantRunInvalid
	}
	side.Identity = map[string]any{"revisionId": source.RevisionID, "candidateDigest": c.Digest, "content": c.Content}
	if source.PublicationID != "" {
		side.Identity["publicationId"] = source.PublicationID
	}
	side.Text = &c.Narrative
	side.Metadata = map[string]any{"kind": "narrative", "title": c.Title, "artifactVersion": c.ArtifactVersion, "workflow": retained.Workflow, "workflowDigest": retained.WorkflowDigest, "completeness": "complete"}
	if c.Source != nil {
		side.Metadata["source"] = c.Source
		side.Metadata["original"] = c.Source.Content
	}
	return side, s.authorized(ctx)
}
func (s policyService) assistantPolicyObservationSide(ctx context.Context, id string, selector assistantPolicySelector) (assistantPolicyComparisonSide, error) {
	side := assistantPolicyComparisonSide{Selector: selector}
	sync, err := s.sourceSync(ctx, id, false)
	if err != nil {
		return side, err
	}
	var held *policySourceObservation
	if sync.State != nil {
		for _, o := range sync.State.Observations {
			if o.ID == selector.ObservationID {
				copy := o
				held = &copy
			}
		}
	}
	if held == nil {
		return side, policyRefusal(404, CodeNotFound, "Source observation not found.")
	}
	// This existing archive verifier checks the retained original, canonical
	// document bytes, source identity and extraction provenance without a fetch.
	if err := validateArchivedPolicyObservation(ctx, s.artifacts, *held); err != nil {
		return side, err
	}
	raw, err := s.artifacts.Get(ctx, held.Document)
	if err != nil {
		return side, err
	}
	record, err := readAssistantDocumentRecord(raw)
	if err != nil {
		return side, err
	}
	var texts []string
	for _, page := range record.pages {
		if page.usable {
			texts = append(texts, page.text)
		}
	}
	if len(texts) > 0 {
		text := strings.Join(texts, "\n\n")
		side.Text = &text
	}
	processing := adObject(record.raw["processing"])
	content := adObject(record.raw["content"])
	completeness := "complete"
	if processing["status"] == "failed" || len(texts) == 0 {
		completeness = "unavailable"
	} else if record.partial {
		completeness = "partial"
	}
	pages := []map[string]any{}
	originalPages, _ := adArray(content["pages"])
	for _, value := range originalPages {
		page := adObject(value)
		out := map[string]any{}
		for _, key := range []string{"number", "status", "extraction", "chars", "unmapped"} {
			out[key] = page[key]
		}
		pages = append(pages, out)
	}
	side.Identity = map[string]any{"observationId": held.ID, "bindingId": held.BindingID, "sourceRevision": held.SourceRevision, "content": held.Document}
	side.Metadata = map[string]any{"kind": "extracted-source", "title": held.Name, "source": held.Source, "original": held.Original, "extraction": held.Extraction, "observedAt": held.ObservedAt, "retainedAt": held.RetainedAt, "processing": map[string]any{"status": processing["status"], "pageCount": content["pageCount"], "truncated": content["truncated"], "pages": pages}, "completeness": completeness}
	return side, s.authorized(ctx)
}
func (s policyService) assistantPolicyComparison(ctx context.Context, id string, beforeRaw, afterRaw json.RawMessage) (any, error) {
	resolve := func(raw json.RawMessage) (assistantPolicyComparisonSide, error) {
		selector, err := readAssistantPolicySelector(raw)
		if err != nil {
			return assistantPolicyComparisonSide{}, err
		}
		if selector.Kind == "observation" {
			return s.assistantPolicyObservationSide(ctx, id, selector)
		}
		return s.assistantPolicySavedSide(ctx, id, selector)
	}
	before, err := resolve(beforeRaw)
	if err != nil {
		return nil, err
	}
	after, err := resolve(afterRaw)
	if err != nil {
		return nil, err
	}
	differences := []map[string]any{}
	same := func(a, b any) bool { x, _ := json.Marshal(a); y, _ := json.Marshal(b); return string(x) == string(y) }
	add := func(field string, a, b any) {
		if !same(a, b) {
			differences = append(differences, map[string]any{"field": field, "before": a, "after": b})
		}
	}
	if !same(before.Text, after.Text) {
		size := func(p *string) any {
			if p == nil {
				return nil
			}
			return map[string]int{"bytes": len(*p)}
		}
		differences = append(differences, map[string]any{"field": "text", "before": size(before.Text), "after": size(after.Text)})
	}
	for _, key := range []string{"candidateDigest", "content", "bindingId", "sourceRevision"} {
		add(key, before.Identity[key], after.Identity[key])
	}
	for _, key := range []string{"title", "artifactVersion", "workflow", "workflowDigest", "source", "original", "extraction", "observedAt", "retainedAt", "processing", "completeness"} {
		add(key, before.Metadata[key], after.Metadata[key])
	}
	limitations := []string{"informational-only"}
	for _, item := range []struct {
		flag bool
		code string
	}{
		{before.Metadata["kind"] == "extracted-source" || after.Metadata["kind"] == "extracted-source", "snapshot-only"},
		{before.Metadata["completeness"] == "partial" || after.Metadata["completeness"] == "partial", "incomplete-extraction"},
		{before.Metadata["completeness"] == "unavailable" || after.Metadata["completeness"] == "unavailable", "unavailable-extraction"},
		{before.Metadata["kind"] != after.Metadata["kind"], "mixed-projections"},
		{before.Identity["observationId"] != nil && after.Identity["observationId"] != nil && (!same(before.Identity["bindingId"], after.Identity["bindingId"]) || !same(before.Metadata["source"], after.Metadata["source"])), "different-source-binding"},
		{before.Metadata["kind"] == "extracted-source" && after.Metadata["kind"] == "extracted-source" && before.Text != nil && same(before.Text, after.Text), "equal-extracted-text"},
	} {
		if item.flag {
			limitations = append(limitations, item.code)
		}
	}
	if err := s.authorized(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"policyId": id, "before": before, "after": after, "differences": differences, "limitations": limitations}, nil
}
